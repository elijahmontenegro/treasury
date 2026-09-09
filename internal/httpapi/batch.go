package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"treasury/api"
	"treasury/ttb"
	"treasury/verify"
)

// VerifyBatch reads a CSV of claims and a ZIP of images and answers
// `application/x-ndjson`: one object per line, written as each item
// finishes.
//
// It streams for two reasons and both matter at three hundred labels.
// A caller sees progress rather than a connection that appears hung for
// several minutes; and nothing accumulates in memory waiting to be
// collected into one document, so the service's footprint is the images
// in flight rather than the batch.
//
// Concurrency is bounded to the core count. The engine is already
// parallel inside one verification (step 29a), so running many at once on
// top of that would oversubscribe every core and make each label slower
// without finishing the batch sooner.
func (s *Server) VerifyBatch(w http.ResponseWriter, r *http.Request) {
	if s.Engine == nil {
		fail(w, http.StatusServiceUnavailable, "The service is not ready to read images yet.")
		return
	}
	max := s.MaxBatch
	if max <= 0 {
		max = 100 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, max+(1<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			fail(w, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("The batch is larger than this service accepts, which is %d MB.", max>>20))
			return
		}
		fail(w, http.StatusBadRequest,
			"The request was not a valid multipart form: send a CSV as 'claims' and a ZIP as 'images'.")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll() // nothing is kept
		}
	}()

	rows, err := readClaimsCSV(r)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	images, closeZip, err := openImagesZip(r)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	defer closeZip()

	// Once the first line is written the status is 200 and cannot be
	// taken back, so everything that could refuse the whole batch is
	// decided above this point. A failure after it belongs to one item
	// and is reported on that item's line.
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	var mu sync.Mutex
	enc := json.NewEncoder(w)
	write := func(item api.BatchItem) {
		mu.Lock()
		defer mu.Unlock()
		_ = enc.Encode(item)
		if flusher != nil {
			flusher.Flush()
		}
	}

	n := runtime.NumCPU()
	if s.Engine != nil {
		n = s.batchWorkers()
	}
	if n > len(rows) {
		n = len(rows)
	}
	if n < 1 {
		n = 1
	}
	var wg sync.WaitGroup
	next := make(chan int)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				write(s.oneOfBatch(r.Context(), rows[i], images))
			}
		}()
	}
	for i := range rows {
		select {
		case next <- i:
		case <-r.Context().Done():
			// The caller hung up. Stop handing out work; what is in
			// flight finishes and is discarded with the request.
			close(next)
			wg.Wait()
			return
		}
	}
	close(next)
	wg.Wait()
}

// batchWorkers is how many labels are read at once.
func (s *Server) batchWorkers() int {
	if s.BatchWorkers > 0 {
		return s.BatchWorkers
	}
	return runtime.NumCPU()
}

// claimRow is one line of the CSV: which image, and what was filed for it.
type claimRow struct {
	num   int
	image string
	app   ttb.Expected
}

// oneOfBatch verifies one row. An error here is this row's, and the batch
// goes on: a ZIP of three hundred labels with one unreadable file should
// not lose the other two hundred and ninety-nine.
func (s *Server) oneOfBatch(ctx context.Context, row claimRow, images map[string]*zip.File) api.BatchItem {
	item := api.BatchItem{Image: row.image, Row: &row.num}
	f, ok := images[strings.ToLower(row.image)]
	if !ok {
		e := "There is no file called " + row.image + " in the ZIP."
		item.Error = &e
		return item
	}
	rc, err := f.Open()
	if err != nil {
		e := row.image + " could not be opened."
		item.Error = &e
		return item
	}
	defer rc.Close()
	img, err := decodeImage(rc, s.Limits.MaxPixels)
	if errors.Is(err, errTooManyPixels) {
		e := row.image + " is far larger than any label and was not decoded."
		item.Error = &e
		return item
	}
	if err != nil {
		e := row.image + " is not a PNG or a JPEG this service can read."
		item.Error = &e
		return item
	}
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}
	refs, claims := ttb.Inputs(row.app)
	start := time.Now()
	res, err := s.Engine.Verify(ctx, img, refs, claims)
	spent := time.Since(start)
	if s.Budget != nil {
		s.Budget.Spend(spent)
	}
	took := int(spent.Milliseconds())
	if err != nil {
		e := row.image + " could not be verified: " + err.Error()
		item.Error = &e
		return item
	}
	item.LatencyMs = &took
	// No crops: three hundred labels of them would be a response measured
	// in gigabytes. Each verdict says whether one exists, and the caller
	// can ask /verify for that label alone to see it.
	item.Claims = verdictsOf(res.Claims, nil)
	if v := verdictsOf(res.Reference, nil); len(*v) > 0 {
		item.Reference = v
	}
	if v := verdictsOf(res.Emphasis, nil); len(*v) > 0 {
		item.Emphasis = v
	}
	markCrops(item.Claims, res.Claims)
	markCrops(item.Reference, res.Reference)
	markCrops(item.Emphasis, res.Emphasis)
	return item
}

// markCrops says, per verdict, whether a crop exists without sending one:
// a batch of three hundred labels' crops would be a response measured in
// gigabytes, and a reviewer looking at one item can ask /verify for it.
func markCrops(out *[]api.Verdict, from []verify.Verdict) {
	if out == nil {
		return
	}
	for i := range *out {
		if i < len(from) && from[i].Evidence != nil &&
			!from[i].Evidence.Region.Empty() && (*out)[i].Evidence != nil {
			yes := true
			(*out)[i].Evidence.HasCrop = &yes
		}
	}
}

// readClaimsCSV reads the CSV of claims, and says plainly what is wrong
// with it when something is.
func readClaimsCSV(r *http.Request) ([]claimRow, error) {
	f, _, err := r.FormFile("claims")
	if err != nil {
		return nil, errors.New("No claims were sent. Attach a CSV as the 'claims' field.")
	}
	defer f.Close()
	cr := csv.NewReader(f)
	cr.FieldsPerRecord = -1
	records, err := cr.ReadAll()
	if err != nil {
		return nil, errors.New("The claims file is not valid CSV: " + err.Error())
	}
	if len(records) < 2 {
		return nil, errors.New("The claims file has a header row and nothing else.")
	}
	head := map[string]int{}
	for i, name := range records[0] {
		head[strings.ToLower(strings.TrimSpace(name))] = i
	}
	if _, ok := head["image"]; !ok {
		return nil, errors.New("The claims file needs a column called 'image', naming a file in the ZIP.")
	}
	at := func(rec []string, name string) string {
		i, ok := head[name]
		if !ok || i >= len(rec) {
			return ""
		}
		return strings.TrimSpace(rec[i])
	}
	var out []claimRow
	for n, rec := range records[1:] {
		name := at(rec, "image")
		if name == "" {
			continue // a blank line at the end of a spreadsheet is not an error
		}
		row := claimRow{num: n + 1, image: name}
		row.app.Beverage = strings.ToLower(at(rec, "beverage"))
		row.app.Brand = at(rec, "brand")
		row.app.Class = at(rec, "class")
		if p := at(rec, "producer"); p != "" {
			row.app.Producer = append(row.app.Producer, p)
			if a := at(rec, "address"); a != "" {
				row.app.Producer = append(row.app.Producer, a)
			}
		}
		row.app.Origin = at(rec, "origin")
		if v, err := strconv.ParseFloat(at(rec, "abv"), 64); err == nil {
			row.app.ABV = v
		}
		if v, err := strconv.ParseFloat(at(rec, "net_ml"), 64); err == nil {
			row.app.NetML = v
		}
		out = append(out, row)
	}
	if len(out) == 0 {
		return nil, errors.New("The claims file names no images.")
	}
	return out, nil
}

// openImagesZip indexes the ZIP by file name, folded to lower case and
// stripped of directories, so a CSV that says `0047.png` finds
// `labels/0047.PNG`.
func openImagesZip(r *http.Request) (map[string]*zip.File, func(), error) {
	f, header, err := r.FormFile("images")
	if err != nil {
		return nil, func() {}, errors.New("No images were sent. Attach a ZIP as the 'images' field.")
	}
	raw, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		return nil, func() {}, errors.New("The images file could not be read.")
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, func() {}, errors.New(header.Filename + " is not a ZIP this service can read.")
	}
	out := map[string]*zip.File{}
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		out[strings.ToLower(path.Base(zf.Name))] = zf
	}
	if len(out) == 0 {
		return nil, func() {}, errors.New("The ZIP holds no files.")
	}
	return out, func() {}, nil
}
