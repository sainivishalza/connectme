"""One-off: export hosamEl1/vit-base-violence-detection (Apache-2.0) to ONNX.

That checkpoint is a *timm* ViT-B/16 (keys such as blocks.0.attn.qkv), not a
Hugging Face transformers one, so loading it with transformers silently leaves
every weight randomly initialised. This script loads it with timm, refuses to
continue unless every weight matched, and checks that the exported ONNX
reproduces the PyTorch logits.

    pip install torch timm safetensors onnxruntime numpy      # throwaway venv
    python tools/export_violence_onnx.py models/violence.onnx <folder with model.safetensors>

Input is 224x224, mean/std 0.5. The class order is NOT documented by the
author; the service assumes ["nonviolence", "violence"] (the alphabetical order
the Kaggle dataset folders produce). Check it with tools/check_violence_labels.py
on a few photos you know before relying on it.
"""
import os
import sys

import numpy as np
import onnxruntime as ort
import timm
import torch
from safetensors.torch import load_file

out = sys.argv[1] if len(sys.argv) > 1 else "models/violence.onnx"
src = sys.argv[2] if len(sys.argv) > 2 else "."
state = load_file(os.path.join(src, "model.safetensors"))

model = timm.create_model("vit_base_patch16_224", pretrained=False, num_classes=2)
missing, unexpected = model.load_state_dict(state, strict=False)
if missing or unexpected:
    sys.exit(f"weights did not match the timm architecture: missing={missing[:5]} unexpected={unexpected[:5]}")
model.eval()
print("loaded every weight; head:", tuple(state["head.weight"].shape))

dummy = torch.randn(1, 3, 224, 224)
torch.onnx.export(model, (dummy,), out, input_names=["pixel_values"], output_names=["logits"],
                  dynamic_axes={"pixel_values": {0: "batch"}, "logits": {0: "batch"}}, opset_version=17, dynamo=False)

x = np.random.default_rng(7).standard_normal((1, 3, 224, 224)).astype("float32")
with torch.no_grad():
    a = model(torch.from_numpy(x)).numpy()
b = ort.InferenceSession(out).run(None, {"pixel_values": x})[0]
diff = float(np.abs(a - b).max())
print("max abs diff vs PyTorch:", diff)
if diff > 1e-3:
    sys.exit("the ONNX file does not reproduce the model; not trusting it")
print("wrote", out)
