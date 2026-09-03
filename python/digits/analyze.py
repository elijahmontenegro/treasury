"""Per-family accuracy of the trained classifier on the heldout frames.

    uv run analyze.py [--model model.pt] [--data data] [--worst 15]
"""

import argparse
import json
from pathlib import Path

import numpy as np
import torch

from train import CLASSES, OTHER, Net, load


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", default="model.pt")
    ap.add_argument("--data", default="data")
    ap.add_argument("--worst", type=int, default=15)
    args = ap.parse_args()

    data = Path(args.data)
    man = json.loads((data / "manifest.json").read_text())
    families = man.get("families")
    x, y = load(data, "heldout", man["side"])
    fam = np.fromfile(data / "heldout.fam", dtype="<u2") if (data / "heldout.fam").exists() else None

    ck = torch.load(args.model, map_location="cpu")
    net = Net()
    net.load_state_dict(ck["state"])
    net.eval()
    preds = []
    with torch.no_grad():
        for i in range(0, len(y), 2048):
            preds.append(net(x[i:i + 2048].float() / 255).argmax(1).numpy())
    pred = np.concatenate(preds)
    y = y.numpy()
    digit = y < OTHER
    print(f"heldout digit accuracy {(pred[digit] == y[digit]).mean():.4f} over {digit.sum()} digit-class frames")

    if fam is not None and families:
        rows = []
        for f in np.unique(fam):
            m = (fam == f) & digit
            if m.sum() == 0:
                continue
            rows.append((families[f], (pred[m] == y[m]).mean(), int(m.sum())))
        rows.sort(key=lambda r: r[1])
        print(f"worst {args.worst} families by digit accuracy:")
        for name, acc, n in rows[:args.worst]:
            print(f"  {name:34s} {acc:.3f}  ({n} frames)")
        above = [r for r in rows if r[1] >= 0.98]
        print(f"{len(above)}/{len(rows)} families at or above 0.98")

    conf = np.zeros((len(CLASSES), len(CLASSES)), dtype=np.int64)
    for t, p in zip(y, pred):
        conf[t, p] += 1
    pairs = [(CLASSES[t], CLASSES[p], int(conf[t, p])) for t in range(OTHER) for p in range(len(CLASSES)) if t != p and conf[t, p] > 0]
    pairs.sort(key=lambda r: -r[2])
    print("worst confusions (true -> predicted):")
    for t, p, n in pairs[:12]:
        print(f"  {t!r} -> {p!r}: {n}")


if __name__ == "__main__":
    main()
