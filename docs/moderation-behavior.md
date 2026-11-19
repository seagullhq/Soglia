# Behavior notes

← [API documentation index](../api/README.md)

- Tier 1 is an exact/normalized word-boundary blocklist check against
  `v6/clean_blocklist.txt`.
- Tier 2 runs `model_int8.onnx` with `tokenizer.json` (max 64 tokens), then
  applies the temperature and thresholds from `v6/calibration.json`
  (`block_threshold = 0.65`, `review_threshold = 0.6`).
- The `matched` word is returned to callers on every `block`/`review`:
  - **Tier 1** — the exact normalized blocklist token. Normalization can change
    what was typed, e.g. `v a f f a n c u l o` and `stfu you absolute m0r0n`
    report `vaffanculo` and `stfu`.
  - **Tier 2** — the model scores the whole message, so `matched` is an
    attribution: each non-stopword is blanked in turn and the word causing the
    largest score drop is returned (e.g. `Che scazzottata ieri` → `scazzottata`).
    When several toxic words dilute that signal, each word is scored alone and
    the most toxic one is used (e.g. `You are such a f**king idiot` → `f**king`).
    Punctuation-obfuscated words stay intact (`f**king`, not `king`). If no word
    clears the threshold the field is empty. The UI labels this as "likely word"
    to distinguish it from an exact blocklist hit.
- Text normalization (homoglyphs, leetspeak, invisible characters, repeated
  characters) is identical to the reference `train_data/test_local.py`.
- Requests are serialized through the single worker (inference takes ~3 ms),
  so batch requests run sequentially.
- CORS headers are reflected only for local origins (same host or
  `localhost`/`127.0.0.1`), with `Allow-Credentials` enabled so the Vite dev
  server can forward the session cookie.
