"""Train the digit-class glyph classifier.

Data comes from `go run ./cmd/digits gen` as raw Side×Side bytes with one
label byte per frame; families are split between train and heldout by the
generator, so the heldout accuracy is on faces the model never saw.

    uv run train.py [--epochs 20] [--data data] [--out model.pt]
"""

import argparse
import json
import time
from pathlib import Path

import numpy as np
import torch
import torch.nn as nn
import torch.nn.functional as F

CLASSES = "0123456789%.,?"
OTHER = len(CLASSES) - 1


def load(data: Path, name: str, side: int):
    pix = np.fromfile(data / f"{name}.u8", dtype=np.uint8)
    lbl = np.fromfile(data / f"{name}.lbl", dtype=np.uint8)
    n = len(lbl)
    pix = pix.reshape(n, 1, side, side)
    return torch.from_numpy(pix), torch.from_numpy(lbl.astype(np.int64))


class Net(nn.Module):
    """Three blocks of two 3×3 convolutions with batch norm, each followed by
    a 2×2 max pool, then global average pooling and a linear head. About 51k
    parameters at widths 16, 32, 48."""

    def __init__(self, widths=(16, 32, 48), classes=len(CLASSES)):
        super().__init__()
        layers = []
        cin = 1
        for w in widths:
            layers += [
                nn.Conv2d(cin, w, 3, padding=1, bias=False), nn.BatchNorm2d(w), nn.ReLU(inplace=True),
                nn.Conv2d(w, w, 3, padding=1, bias=False), nn.BatchNorm2d(w), nn.ReLU(inplace=True),
                nn.MaxPool2d(2),
            ]
            cin = w
        self.features = nn.Sequential(*layers)
        self.head = nn.Linear(cin, classes)

    def forward(self, x):
        x = self.features(x)
        x = x.mean(dim=(2, 3))
        return self.head(x)


def augment(x: torch.Tensor) -> torch.Tensor:
    """Light on-the-fly jitter on top of the generator's channel: a shift of
    up to two pixels and a contrast scale. The generator already varied
    geometry, blur, compression, and lighting."""
    n = x.shape[0]
    dx = torch.randint(-2, 3, (n,))
    dy = torch.randint(-2, 3, (n,))
    out = torch.empty_like(x)
    for sy in range(-2, 3):
        for sx in range(-2, 3):
            sel = (dx == sx) & (dy == sy)
            if sel.any():
                out[sel] = torch.roll(x[sel], shifts=(sy, sx), dims=(2, 3))
    scale = 0.8 + 0.4 * torch.rand(n, 1, 1, 1)
    return (out * scale).clamp(0, 1)


def evaluate(model, x, y, device, batch=1024):
    model.eval()
    correct = np.zeros(len(CLASSES), dtype=np.int64)
    total = np.zeros(len(CLASSES), dtype=np.int64)
    conf = np.zeros((len(CLASSES), len(CLASSES)), dtype=np.int64)
    with torch.no_grad():
        for i in range(0, len(y), batch):
            xb = x[i:i + batch].to(device).float() / 255
            yb = y[i:i + batch].numpy()
            pred = model(xb).argmax(1).cpu().numpy()
            for t, p in zip(yb, pred):
                total[t] += 1
                conf[t, p] += 1
                if t == p:
                    correct[t] += 1
    digit_mask = np.arange(len(CLASSES)) < OTHER
    digit_acc = correct[digit_mask].sum() / max(1, total[digit_mask].sum())
    all_acc = correct.sum() / max(1, total.sum())
    return digit_acc, all_acc, correct, total, conf


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--data", default="data")
    ap.add_argument("--out", default="model.pt")
    ap.add_argument("--epochs", type=int, default=20)
    ap.add_argument("--batch", type=int, default=256)
    ap.add_argument("--lr", type=float, default=2e-3)
    ap.add_argument("--seed", type=int, default=1)
    args = ap.parse_args()

    torch.manual_seed(args.seed)
    data = Path(args.data)
    man = json.loads((data / "manifest.json").read_text())
    # The partition is the protocol: a family that may set an evaluated label
    # must never be trained on. Read the same file the generators read.
    part = json.loads((Path(__file__).resolve().parents[2] / "internal" / "fontset" / "partition.json").read_text())
    evaluation = set(part["evaluation"])
    leaked = sorted(evaluation.intersection(man["train_families"]))
    if leaked:
        raise SystemExit("training data draws on evaluation families: " + ", ".join(leaked))
    side = man["side"]
    assert man["classes"] == CLASSES, man["classes"]
    xtr, ytr = load(data, "train", side)
    xhe, yhe = load(data, "heldout", side)
    print(f"train {len(ytr)} heldout {len(yhe)} side {side} heldout families {len(man['heldout_families'])}")

    device = torch.device("cuda" if torch.cuda.is_available() else "cpu")
    model = Net().to(device)
    params = sum(p.numel() for p in model.parameters())
    print(f"params {params} device {device}")

    opt = torch.optim.AdamW(model.parameters(), lr=args.lr, weight_decay=1e-4)
    steps = args.epochs * ((len(ytr) + args.batch - 1) // args.batch)
    sched = torch.optim.lr_scheduler.OneCycleLR(opt, max_lr=args.lr, total_steps=steps)
    best = 0.0
    for epoch in range(args.epochs):
        model.train()
        perm = torch.randperm(len(ytr))
        t0 = time.time()
        loss_sum, seen = 0.0, 0
        for i in range(0, len(perm), args.batch):
            idx = perm[i:i + args.batch]
            xb = augment(xtr[idx].float() / 255).to(device)
            yb = ytr[idx].to(device)
            logits = model(xb)
            loss = F.cross_entropy(logits, yb, label_smoothing=0.05)
            opt.zero_grad(set_to_none=True)
            loss.backward()
            opt.step()
            sched.step()
            loss_sum += float(loss.detach()) * len(idx)
            seen += len(idx)
        digit_acc, all_acc, _, _, _ = evaluate(model, xhe, yhe, device)
        print(f"epoch {epoch + 1:2d} loss {loss_sum / seen:.4f} heldout digit acc {digit_acc:.4f} all {all_acc:.4f} ({time.time() - t0:.0f}s)")
        if digit_acc >= best:
            best = digit_acc
            torch.save({"state": model.state_dict(), "classes": CLASSES, "side": side}, args.out)

    ck = torch.load(args.out, map_location=device)
    model.load_state_dict(ck["state"])
    digit_acc, all_acc, correct, total, conf = evaluate(model, xhe, yhe, device)
    print(f"best heldout digit accuracy {digit_acc:.4f} (all classes {all_acc:.4f})")
    for c in range(len(CLASSES)):
        wrong = [(CLASSES[p], int(conf[c, p])) for p in np.argsort(-conf[c]) if p != c and conf[c, p] > 0][:3]
        print(f"  {CLASSES[c]!r}: {correct[c]}/{total[c]} = {correct[c] / max(1, total[c]):.4f}  confused with {wrong}")


if __name__ == "__main__":
    main()
