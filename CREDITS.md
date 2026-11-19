# Credits

Soglia is built on the work of the open-source community. This file records the
upstream models, datasets and libraries used to train and run it.

> The trained `v6` model is **not included in this repository** and will be
> released later. It must be supplied at runtime (`/app/v6` in the container,
> `../v6` for local development). See [README.md](README.md).

## Model and training data

The `v6` classifier is fine-tuned from:

- **[`FacebookAI/xlm-roberta-base`](https://huggingface.co/FacebookAI/xlm-roberta-base)**
  — the multilingual XLM-RoBERTa base model by Facebook AI Research
  ([MIT license](https://huggingface.co/FacebookAI/xlm-roberta-base/blob/main/README.md)).
  Described in Conneau et al., *Unsupervised Cross-lingual Representation
  Learning at Scale* ([arXiv:1911.02116](https://arxiv.org/abs/1911.02116)).
- **[`shawarmas/profanity-filter`](https://huggingface.co/datasets/shawarmas/profanity-filter)**
  — the dataset used to train the `v6` model.
- **[`textdetox/multilingual_toxicity_dataset`](https://huggingface.co/datasets/textdetox/multilingual_toxicity_dataset)**
  — the non-toxic examples used to balance the training set
  ([OpenRAIL++ license](https://huggingface.co/datasets/textdetox/multilingual_toxicity_dataset)).

The positive (profanity) examples come from the local word lists in
`train_data/` (`this.txt`, `list.jsonl` and their cleaned/augmented variants).
The cleaning, augmentation, training and evaluation scripts are also in
`train_data/`. Text normalization in `api/worker.py` follows the project's
earlier Rust/Python reference implementation.

## Runtime dependencies

### Go API

- [Go standard library](https://go.dev/) — HTTP server, TLS, process and signal handling
- [pgx v5](https://github.com/jackc/pgx) — PostgreSQL driver
- [modernc.org/sqlite](https://modernc.org/sqlite) — pure-Go SQLite driver used by the one-time migration tool

### Python inference worker

- [NumPy](https://numpy.org/) — array handling for model inputs
- [ONNX Runtime](https://onnxruntime.ai/) — CPU inference for the quantized model
- [Hugging Face Tokenizers](https://github.com/huggingface/tokenizers) — fast tokenizer runtime

Training additionally uses [PyTorch](https://pytorch.org/),
[Hugging Face Transformers](https://github.com/huggingface/transformers) and
[Datasets](https://github.com/huggingface/datasets).

### Management console

- [React](https://react.dev/) and [React DOM](https://react.dev/)
- [Vite](https://vite.dev/) and [@vitejs/plugin-react](https://github.com/vitejs/vite-plugin-react)
- [TypeScript](https://www.typescriptlang.org/)

## Licensing

Each dependency keeps its own upstream license; refer to the linked project or
the `LICENSE` file shipped with the installed package. This repository does not
currently include a project-wide `LICENSE` file.
