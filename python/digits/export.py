"""Export the trained classifier for the Go engine.

Writes, into ../../internal/digits/:
  model.onnx  the network with batch norm folded, input [N,1,32,32] float32
              in [0,1], output logits [N,14]
  model.bin   the same folded weights as little-endian float32, in layer order
  model.json  the layer list with shapes, so the pure-Go forward pass can
              read model.bin without ONNX
and checks that the folded network, the ONNX file run through onnxruntime,
and the original agree on the heldout set.

    uv run export.py [--model model.pt] [--data data]
"""

import argparse
import json
import struct
from pathlib import Path

import numpy as np
import onnxruntime as ort
import torch
import torch.nn as nn

from train import CLASSES, Net, load


class Folded(nn.Module):
    """Net with every batch norm folded into the convolution before it."""

    def __init__(self, net: Net):
        super().__init__()
        layers = []
        mods = list(net.features)
        i = 0
        while i < len(mods):
            m = mods[i]
            if isinstance(m, nn.Conv2d):
                bn = mods[i + 1]
                assert isinstance(bn, nn.BatchNorm2d)
                scale = bn.weight / torch.sqrt(bn.running_var + bn.eps)
                conv = nn.Conv2d(m.in_channels, m.out_channels, 3, padding=1, bias=True)
                conv.weight.data = m.weight.data * scale.view(-1, 1, 1, 1)
                conv.bias.data = bn.bias.data - bn.running_mean * scale
                layers += [conv, nn.ReLU()]
                i += 3
            elif isinstance(m, nn.MaxPool2d):
                layers.append(nn.MaxPool2d(2))
                i += 1
            else:
                raise TypeError(m)
        self.features = nn.Sequential(*layers)
        self.head = nn.Linear(net.head.in_features, net.head.out_features)
        self.head.weight.data = net.head.weight.data.clone()
        self.head.bias.data = net.head.bias.data.clone()

    def forward(self, x):
        x = self.features(x)
        x = x.mean(dim=(2, 3))
        return self.head(x)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--model", default="model.pt")
    ap.add_argument("--data", default="data")
    ap.add_argument("--out", default="../../internal/digits")
    args = ap.parse_args()

    ck = torch.load(args.model, map_location="cpu")
    net = Net()
    net.load_state_dict(ck["state"])
    net.eval()
    folded = Folded(net).eval()
    side = ck["side"]

    xhe, yhe = load(Path(args.data), "heldout", side)
    x = xhe[:4096].float() / 255
    with torch.no_grad():
        a = net(x)
        b = folded(x)
    print(f"fold max abs diff {(a - b).abs().max():.2e}")

    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    onnx_path = out / "model.onnx"
    torch.onnx.export(
        folded, x[:1], str(onnx_path), input_names=["frame"], output_names=["logits"],
        dynamic_axes={"frame": {0: "n"}, "logits": {0: "n"}}, opset_version=17, dynamo=False,
    )
    sess = ort.InferenceSession(str(onnx_path), providers=["CPUExecutionProvider"])
    c = sess.run(None, {"frame": x.numpy()})[0]
    print(f"onnx max abs diff {np.abs(c - b.numpy()).max():.2e}")

    # Raw weights in layer order for the Go forward pass.
    layers = []
    blob = bytearray()

    def put(t: torch.Tensor):
        arr = t.detach().cpu().numpy().astype("<f4").ravel()
        blob.extend(arr.tobytes())
        return int(arr.size)

    for m in folded.features:
        if isinstance(m, nn.Conv2d):
            layers.append({"kind": "conv", "in": m.in_channels, "out": m.out_channels, "k": 3, "weights": put(m.weight), "bias": put(m.bias)})
        elif isinstance(m, nn.ReLU):
            layers.append({"kind": "relu"})
        elif isinstance(m, nn.MaxPool2d):
            layers.append({"kind": "maxpool", "k": 2})
    layers.append({"kind": "gap"})
    layers.append({"kind": "linear", "in": folded.head.in_features, "out": folded.head.out_features, "weights": put(folded.head.weight), "bias": put(folded.head.bias)})
    (out / "model.bin").write_bytes(bytes(blob))
    (out / "model.json").write_text(json.dumps({"side": side, "classes": CLASSES, "layers": layers, "floats": len(blob) // 4}, indent=1))

    # Heldout accuracy through onnxruntime, the number the Go side must reproduce.
    correct = total = 0
    for i in range(0, len(yhe), 2048):
        xb = (xhe[i:i + 2048].float() / 255).numpy()
        yb = yhe[i:i + 2048].numpy()
        pred = sess.run(None, {"frame": xb})[0].argmax(1)
        digit = yb < len(CLASSES) - 1
        correct += int((pred[digit] == yb[digit]).sum())
        total += int(digit.sum())
    print(f"heldout digit accuracy via onnxruntime {correct / total:.4f} ({correct}/{total})")
    print(f"wrote {onnx_path} ({onnx_path.stat().st_size} bytes), model.bin ({len(blob)} bytes), model.json ({len(layers)} layers)")
    struct  # keep import explicit for readers of the binary layout: little-endian float32, no header


if __name__ == "__main__":
    main()
