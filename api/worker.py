#!/usr/bin/env python3
"""JSON-lines inference worker for the profanity-filter model.

The Go API starts this process once, keeps it alive, and sends one JSON object
per request on stdin. Each request produces exactly one JSON response line on
stdout. All diagnostics go to stderr so the stdout protocol stays clean.

Request:  {"id": 1, "text": "some message"}
Response: {"id": 1, "action": "block", "tier": "Tier 1", "prob": 1.0,
           "matched": "ricione", "ms": 0.12}

For Tier 1, "matched" is the exact normalized blocklist token. For Tier 2,
it is the word whose removal lowered the toxicity score the most (leave-one-out
attribution), so callers always get the offending word on block/review.
"""

import json
import math
import os
import re
import sys
import time
import unicodedata

import numpy as np
import onnxruntime as ort
from tokenizers import Tokenizer


# --------------------------------------------------------------------------- #
# Loading                                                                      #
# --------------------------------------------------------------------------- #

def resolve_model_dir() -> str:
    if len(sys.argv) > 1:
        return sys.argv[1]
    if os.environ.get("MODEL_DIR"):
        return os.environ["MODEL_DIR"]
    return os.path.normpath(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "v6"))


MODEL_DIR = resolve_model_dir()

with open(os.path.join(MODEL_DIR, "calibration.json"), "r", encoding="utf-8") as f:
    CAL = json.load(f)


def apply_calibration_overrides(raw):
    """Merge Setup-page overrides over the values from calibration.json."""
    if not raw:
        return
    try:
        overrides = json.loads(raw)
    except ValueError:
        print("Ignoring invalid calibration overrides", file=sys.stderr, flush=True)
        return
    for key in ("temperature", "block_threshold", "review_threshold"):
        if key not in overrides:
            continue
        try:
            value = float(overrides[key])
        except (TypeError, ValueError):
            print(f"Ignoring invalid calibration override for {key}", file=sys.stderr, flush=True)
            continue
        if not math.isfinite(value) or value <= 0 or (key != "temperature" and value > 1):
            print(f"Ignoring out-of-range calibration override for {key}", file=sys.stderr, flush=True)
            continue
        CAL[key] = value


apply_calibration_overrides(sys.argv[2] if len(sys.argv) > 2 else None)
print(
    f"Calibration: temperature={CAL['temperature']}, "
    f"review={CAL['review_threshold']}, block={CAL['block_threshold']}",
    file=sys.stderr,
    flush=True,
)

with open(os.path.join(MODEL_DIR, "clean_blocklist.txt"), "r", encoding="utf-8") as f:
    BLOCKLIST = set(line.strip() for line in f if line.strip())

TOKENIZER = Tokenizer.from_file(os.path.join(MODEL_DIR, "tokenizer.json"))
TOKENIZER.enable_truncation(max_length=64)

SESSION_OPTIONS = ort.SessionOptions()
SESSION_OPTIONS.graph_optimization_level = ort.GraphOptimizationLevel.ORT_ENABLE_ALL
SESSION_OPTIONS.intra_op_num_threads = 1
SESSION = ort.InferenceSession(
    os.path.join(MODEL_DIR, "model_int8.onnx"),
    sess_options=SESSION_OPTIONS,
    providers=["CPUExecutionProvider"],
)

# Warm up so the first real request does not pay JIT/allocation overhead.
_WARMUP = TOKENIZER.encode("warmup message")
SESSION.run(
    ["logits"],
    {
        "input_ids": np.array([_WARMUP.ids], dtype=np.int64),
        "attention_mask": np.array([_WARMUP.attention_mask], dtype=np.int64),
    },
)

print(f"Loaded model from {MODEL_DIR} ({len(BLOCKLIST)} blocklist entries)", file=sys.stderr, flush=True)


# --------------------------------------------------------------------------- #
# Normalization (kept identical to the Rust/Python reference implementation)   #
# --------------------------------------------------------------------------- #

CHAR_MAP = str.maketrans(
    {
        "а": "a", "е": "e", "о": "o", "р": "p", "с": "c", "у": "y", "х": "x", "і": "i",
        "α": "a", "ε": "e", "ο": "o", "ρ": "p", "τ": "t", "υ": "u", "χ": "x",
        "@": "a", "4": "a", "3": "e", "1": "i", "!": "i", "|": "i",
        "0": "o", "$": "s", "5": "s", "7": "t", "8": "b",
    }
)
SPACED_CHARS_RE = re.compile(r"\b(?:[a-z0-9]\s*[.\-_*\s]\s*){2,}[a-z0-9]\b")
INVISIBLE_CHARS_RE = re.compile(r"[\u200b-\u200f\u202a-\u202e\u2060\ufeff\xad]")
TOKEN_RE = re.compile(r"[a-z0-9]+")
REPEAT_RE = re.compile(r"(.)\1+")
TRIPLE_RE = re.compile(r"(.)\1{2,}")
# Treat "f**king" / "w-h-a-t" as one word so attribution reports what the user
# typed instead of a fragment.
WORD_RE = re.compile(r"[^\W_]+(?:[*._\-'][^\W_]*)*", re.UNICODE)
MAX_ATTRIBUTION_WORDS = 20
MIN_ATTRIBUTION_DROP = 0.01
MIN_STANDALONE_PROB = 0.30
STOPWORDS = frozenset(
    """a an the and or but is are was were be been being to of in on at it its
    this that these those you your yours i me my he him his she her they them
    we our us such so very just for with as have has had do does did not no if
    then than there here what when where who which about into over after before""".split()
)


def normalize_text(text: str) -> str:
    text = INVISIBLE_CHARS_RE.sub("", text)
    text = unicodedata.normalize("NFKD", text)
    text = "".join(ch for ch in text if not unicodedata.combining(ch))
    text = text.lower().translate(CHAR_MAP)
    text = SPACED_CHARS_RE.sub(lambda m: re.sub(r"[^a-z0-9]", "", m.group(0)), text)
    text = TRIPLE_RE.sub(r"\1\1", text)
    return text.strip()


# --------------------------------------------------------------------------- #
# Moderation                                                                   #
# --------------------------------------------------------------------------- #

def toxicity_probability(norm: str) -> float:
    """Toxicity score for already-normalized text."""
    if not norm:
        return 0.0
    enc = TOKENIZER.encode(norm)
    logits = SESSION.run(
        ["logits"],
        {
            "input_ids": np.array([enc.ids], dtype=np.int64),
            "attention_mask": np.array([enc.attention_mask], dtype=np.int64),
        },
    )[0][0]
    scaled = logits / CAL["temperature"]
    exp_l = np.exp(scaled - np.max(scaled))
    return float((exp_l / np.sum(exp_l))[1])


def attribute_word(raw_text: str, baseline: float) -> str:
    """Return the word whose removal lowers the toxicity score the most.

    The classifier scores the whole message, so it has no native token-level
    explanation. Scoring again with each word blanked gives callers a practical
    answer for what to fix. Long messages are skipped to bound the extra work.
    """
    words = [m for m in WORD_RE.finditer(raw_text) if m.group(0).lower() not in STOPWORDS]
    if not words or len(words) > MAX_ATTRIBUTION_WORDS:
        return ""

    # Preferred signal: which word, when blanked, drops the score the most.
    best_word = ""
    best_drop = 0.0
    for word in words:
        masked = raw_text[: word.start()] + " " + raw_text[word.end() :]
        drop = baseline - toxicity_probability(normalize_text(masked))
        if drop > best_drop:
            best_drop = drop
            best_word = word.group(0)
    if best_drop >= MIN_ATTRIBUTION_DROP:
        return best_word

    # Fallback for messages with several toxic words: removing any one of them
    # barely changes the score, so score each word on its own instead.
    best_word = ""
    best_score = 0.0
    for word in words:
        score = toxicity_probability(normalize_text(word.group(0)))
        if score > best_score:
            best_score = score
            best_word = word.group(0)
    return best_word if best_score >= MIN_STANDALONE_PROB else ""


def moderate_message(raw_text: str) -> dict:
    t0 = time.perf_counter()
    norm = normalize_text(raw_text)

    if not norm:
        return {"action": "allow", "tier": "FastPath", "prob": 0.0, "ms": 0.0}

    # Tier 1: word-boundary blocklist lookup.
    for tok in TOKEN_RE.findall(norm):
        collapsed = REPEAT_RE.sub(r"\1", tok)
        if tok in BLOCKLIST or (len(collapsed) >= 3 and collapsed in BLOCKLIST):
            return {
                "action": "block",
                "tier": "Tier 1",
                "prob": 1.0,
                "matched": tok,
                "ms": round((time.perf_counter() - t0) * 1000, 3),
            }

    if len(norm) <= 3:
        return {
            "action": "allow",
            "tier": "FastPath",
            "prob": 0.0,
            "ms": round((time.perf_counter() - t0) * 1000, 3),
        }

    # Tier 2: ONNX INT8 transformer + temperature-scaled probabilities.
    toxic_prob = toxicity_probability(norm)

    if toxic_prob >= CAL["block_threshold"]:
        action = "block"
    elif toxic_prob >= CAL["review_threshold"]:
        action = "review"
    else:
        action = "allow"

    result = {
        "action": action,
        "tier": "Tier 2",
        "prob": round(toxic_prob, 4),
        "ms": round((time.perf_counter() - t0) * 1000, 3),
    }
    if action in ("block", "review"):
        result["matched"] = attribute_word(raw_text, toxic_prob)
    return result


# --------------------------------------------------------------------------- #
# JSON-lines loop                                                              #
# --------------------------------------------------------------------------- #

def emit(response: dict) -> None:
    sys.stdout.write(json.dumps(response, ensure_ascii=False, separators=(",", ":")) + "\n")
    sys.stdout.flush()


for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    request = {}
    try:
        request = json.loads(line)
        response = moderate_message(request["text"])
        response["id"] = request.get("id", 0)
    except Exception as exc:  # keep the worker alive on bad input
        response = {"id": 0, "error": f"{type(exc).__name__}: {exc}"}
        try:
            response["id"] = request.get("id", 0)
        except Exception:
            pass
        print(f"request failed: {response['error']}", file=sys.stderr, flush=True)
    emit(response)
