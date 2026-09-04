"""Train the glyph encoder: a 256-bit code of a glyph frame that is the same
for the same character in different faces and different for different
characters.

Data comes from `go run ./cmd/glyphs gen`: binary glyph frames at the
alphabet's own geometry, labelled by character and face family, with the
families split between train and heldout by the generator.

The loss is supervised contrastive over the batch (positives: the same
character in any face, through any augmentation) on the cosine of the
tanh embeddings, plus a quantization term pulling each coordinate towards
±1 so that the sign bits lose little. The report is what the engine relies
on: on held-out families, the nearest character by Hamming distance to
per-character prototypes built from training faces, and the distributions
of the same-character and nearest-other-character distances.

    uv run python -u train.py [--epochs 15] [--data data] [--out model.pt]
"""

import argparse
import json
import time
from pathlib import Path

import numpy as np
import torch
import torch.nn as nn
import torch.nn.functional as F

DIM = 256


def load(data: Path, name: str, side: int):
    pix = np.fromfile(data / f"{name}.u8", dtype=np.uint8)
    lbl = np.fromfile(data / f"{name}.lbl", dtype=np.uint8)
    fam = np.fromfile(data / f"{name}.fam", dtype="<u2")
    n = len(lbl)
    return (torch.from_numpy(pix.reshape(n, 1, side, side)), torch.from_numpy(lbl.astype(np.int64)), torch.from_numpy(fam.astype(np.int64)))


class Net(nn.Module):
    """Four blocks of 3×3 convolutions with batch norm (one in the first
    block, two in the others), each followed by a 2×2 max pool, global
    average pooling, a linear layer to DIM, and tanh. About 4.5 million
    multiply-adds a frame, half the first version's, for the engine's
    latency budget."""

    def __init__(self, widths=(12, 24, 40, 64), dim=DIM):
        super().__init__()
        layers = []
        cin = 1
        for i, w in enumerate(widths):
            layers += [nn.Conv2d(cin, w, 3, padding=1, bias=False), nn.BatchNorm2d(w), nn.ReLU(inplace=True)]
            if i > 0:
                # The first block, at full resolution, is one convolution:
                # the second one there was a quarter of the network's cost.
                layers += [nn.Conv2d(w, w, 3, padding=1, bias=False), nn.BatchNorm2d(w), nn.ReLU(inplace=True)]
            layers.append(nn.MaxPool2d(2))
            cin = w
        self.features = nn.Sequential(*layers)
        self.head = nn.Linear(cin, dim)

    def forward(self, x):
        x = self.features(x)
        x = x.mean(dim=(2, 3))
        return torch.tanh(self.head(x))


def augment(x: torch.Tensor) -> torch.Tensor:
    n = x.shape[0]
    dx = torch.randint(-2, 3, (n,))
    dy = torch.randint(-2, 3, (n,))
    out = torch.empty_like(x)
    for sy in range(-2, 3):
        for sx in range(-2, 3):
            sel = (dx == sx) & (dy == sy)
            if sel.any():
                out[sel] = torch.roll(x[sel], shifts=(sy, sx), dims=(2, 3))
    return out


def supcon(z: torch.Tensor, y: torch.Tensor, temperature: float = 0.1) -> torch.Tensor:
    """Supervised contrastive loss: every same-label pair in the batch is a
    positive; the anchor itself is excluded."""
    z = F.normalize(z, dim=1)
    sim = z @ z.t() / temperature
    n = z.shape[0]
    eye = torch.eye(n, dtype=torch.bool, device=z.device)
    sim = sim.masked_fill(eye, -1e9)
    pos = (y[:, None] == y[None, :]) & ~eye
    log_prob = sim - torch.logsumexp(sim, dim=1, keepdim=True)
    npos = pos.sum(1)
    valid = npos > 0
    loss = -(log_prob * pos).sum(1)[valid] / npos[valid]
    return loss.mean()


def codes(model, x, device, batch=2048):
    """Sign bits of the embeddings, as the engine will use them."""
    model.eval()
    out = []
    with torch.no_grad():
        for i in range(0, len(x), batch):
            z = model(x[i:i + batch].to(device).float() / 255)
            out.append((z > 0).cpu())
    return torch.cat(out)


def evaluate(model, xtr, ytr, xhe, yhe, device, classes):
    """Nearest-prototype accuracy on heldout families and the Hamming
    distributions the engine's radius and tie margin depend on."""
    ctr = codes(model, xtr, device)
    che = codes(model, xhe, device)
    nclass = len(classes)
    proto = torch.zeros(nclass, DIM)
    for c in range(nclass):
        m = ytr == c
        if m.any():
            proto[c] = (ctr[m].float().mean(0) > 0.5).float()
    # Hamming distance from every heldout code to every prototype, normalized.
    che_f = che.float()
    d = (che_f @ (1 - proto).t() + (1 - che_f) @ proto.t()) / DIM  # [n, nclass]
    pred = d.argmin(1)
    acc = (pred == yhe).float().mean().item()
    same = d[torch.arange(len(yhe)), yhe]
    d_other = d.clone()
    d_other[torch.arange(len(yhe)), yhe] = 2
    nearest_other = d_other.min(1).values
    return acc, same, nearest_other


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--data", default="data")
    ap.add_argument("--out", default="model.pt")
    ap.add_argument("--epochs", type=int, default=15)
    ap.add_argument("--batch", type=int, default=512)
    ap.add_argument("--lr", type=float, default=2e-3)
    ap.add_argument("--quant", type=float, default=0.1)
    ap.add_argument("--seed", type=int, default=1)
    args = ap.parse_args()

    torch.manual_seed(args.seed)
    data = Path(args.data)
    man = json.loads((data / "manifest.json").read_text())
    side, classes = man["side"], man["classes"]
    xtr, ytr, _ = load(data, "train", side)
    xhe, yhe, _ = load(data, "heldout", side)
    print(f"train {len(ytr)} heldout {len(yhe)} side {side} classes {len(classes)} heldout families {len(man['heldout_families'])}")

    device = torch.device("cuda" if torch.cuda.is_available() else "cpu")
    model = Net().to(device)
    print(f"params {sum(p.numel() for p in model.parameters())} device {device}")

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
            z = model(xb)
            loss = supcon(z, yb) + args.quant * ((1 - z.abs()) ** 2).mean()
            opt.zero_grad(set_to_none=True)
            loss.backward()
            opt.step()
            sched.step()
            loss_sum += float(loss.detach()) * len(idx)
            seen += len(idx)
        acc, same, other = evaluate(model, xtr, ytr, xhe, yhe, device, classes)
        print(f"epoch {epoch + 1:2d} loss {loss_sum / seen:.4f} heldout nearest-prototype acc {acc:.4f} same-char Hamming mean {same.mean():.3f} p90 {same.quantile(0.9):.3f} nearest-other mean {other.mean():.3f} p10 {other.quantile(0.1):.3f} ({time.time() - t0:.0f}s)")
        if acc >= best:
            best = acc
            torch.save({"state": model.state_dict(), "side": side, "dim": DIM, "classes": classes}, args.out)

    ck = torch.load(args.out, map_location=device)
    model.load_state_dict(ck["state"])
    acc, same, other = evaluate(model, xtr, ytr, xhe, yhe, device, classes)
    print(f"best heldout nearest-prototype accuracy {acc:.4f}")
    print(f"same-character Hamming: mean {same.mean():.3f} p50 {same.median():.3f} p90 {same.quantile(0.9):.3f}")
    print(f"nearest other character: mean {other.mean():.3f} p10 {other.quantile(0.1):.3f} p50 {other.median():.3f}")
    sep = (other - same)
    print(f"margin (other - same): mean {sep.mean():.3f}, share positive {(sep > 0).float().mean():.3f}")


if __name__ == "__main__":
    main()
