"""Prints both class probabilities of the violence model for the images you give.

The model author did not document which output is "violence". The service
assumes index 1 (VIOLENCE_LABELS=nonviolence,violence). Run this on a few
photos you know are harmless (people at a table, a street, sport) and a few you
know are not, and if index 0 is the high one for the violent ones, set
VIOLENCE_LABELS=violence,nonviolence.

    python tools/check_violence_labels.py photo1.jpg photo2.jpg
"""
import math
import sys

import numpy as np
import onnxruntime as ort
from PIL import Image

sess = ort.InferenceSession("models/violence.onnx", providers=["CPUExecutionProvider"])
for path in sys.argv[1:]:
    im = Image.open(path).convert("RGB").resize((224, 224), Image.Resampling.BICUBIC)
    a = (np.asarray(im, dtype="float32") / 255.0 - 0.5) / 0.5
    logits = sess.run(None, {"pixel_values": np.transpose(a, (2, 0, 1))[None].astype("float32")})[0][0]
    e = [math.exp(x - max(logits)) for x in logits]
    p = [x / sum(e) for x in e]
    print(f"{path}: index0={p[0]:.3f} index1={p[1]:.3f}")
