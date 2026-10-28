"""One-off: export Falconsai/nsfw_image_detection (Apache-2.0) to ONNX.

Needs torch, transformers, onnx (not service dependencies; use a throwaway venv):
    python tools/export_nsfw_onnx.py models/nsfw.onnx [local-folder-with-config.json+model.safetensors]
Then run the service with NSFW_MODEL_PATH=models/nsfw.onnx. The model's
labels are ["normal", "nsfw"] and it expects 224x224, mean/std 0.5.
"""
import sys

import torch
from transformers import AutoModelForImageClassification

out = sys.argv[1] if len(sys.argv) > 1 else "models/nsfw.onnx"
src = sys.argv[2] if len(sys.argv) > 2 else "Falconsai/nsfw_image_detection"  # hub id or a local folder
model = AutoModelForImageClassification.from_pretrained(src).eval()
print("labels:", model.config.id2label)
dummy = torch.randn(1, 3, 224, 224)
torch.onnx.export(
    model, (dummy,), out, input_names=["pixel_values"], output_names=["logits"],
    dynamic_axes={"pixel_values": {0: "batch"}, "logits": {0: "batch"}}, opset_version=17, dynamo=False,
)
print("wrote", out)
