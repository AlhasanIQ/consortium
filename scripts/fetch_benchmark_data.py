#!/usr/bin/env python3
"""
Fetch and normalize benchmark datasets for the local Go benchmark runner.

Output schema (JSONL):
{
  "id": "...",
  "benchmark": "global-mmlu|global-mmlu-lite|mmlu-pro|math-500",
  "split": "test|validation|dev|...",
  "subject": "...",
  "language": "...",
  "question": "...",
  "choices": ["A choice", "B choice", ...],   # MCQA only
  "answer_index": 1,                            # MCQA only
  "answer_label": "B",                        # MCQA label OR compatibility copy of gold_answer
  "gold_answer": "...",                       # non-MCQA only
  "gold_solution": "..."                      # non-MCQA only (optional)
}
"""

import argparse
import json
import os
import re
from typing import Any, Dict, Iterable, List, Optional, Sequence, Tuple


BENCHMARK_SPECS = {
    "global-mmlu": {
        "hf_datasets": [
            "CohereLabs/Global-MMLU",
            "CohereForAI/Global-MMLU",
        ],
        "hf_configs": [None, "en"],
        "output_dir": "global_mmlu",
    },
    "global-mmlu-lite": {
        "hf_datasets": ["CohereLabs/Global-MMLU-Lite"],
        "hf_configs": ["en", None],
        "output_dir": "global_mmlu_lite",
    },
    "mmlu-pro": {
        "hf_datasets": ["TIGER-Lab/MMLU-Pro"],
        "hf_configs": [None],
        "output_dir": "mmlu_pro",
    },
    "math-500": {
        "hf_datasets": ["HuggingFaceH4/MATH-500"],
        "hf_configs": [None],
        "output_dir": "math_500",
        "source_split": "test",
        "synthetic_dev_limit": 100,
    },
}

LETTER_TO_INDEX = {chr(ord("A") + i): i for i in range(26)}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--benchmark",
        default="all",
        help="global-mmlu | global-mmlu-lite | mmlu-pro | math-500 | all",
    )
    parser.add_argument(
        "--output-dir",
        default="benchmarks/data",
        help="Base output directory",
    )
    parser.add_argument(
        "--splits",
        default="all",
        help="Comma-separated split list, or 'all' for all available splits",
    )
    parser.add_argument(
        "--limit",
        type=int,
        default=0,
        help="Max rows per split (0 = all)",
    )
    parser.add_argument(
        "--strict",
        action="store_true",
        help="Fail immediately on any row parse error",
    )
    return parser.parse_args()


def parse_benchmarks(raw: str) -> List[str]:
    value = raw.strip().lower()
    if value == "all":
        return list(BENCHMARK_SPECS.keys())
    items = [x.strip().lower() for x in raw.split(",") if x.strip()]
    for item in items:
        if item not in BENCHMARK_SPECS:
            raise ValueError(f"Unsupported benchmark '{item}'")
    return items


def parse_splits(raw: str) -> Optional[List[str]]:
    value = raw.strip().lower()
    if value == "all":
        return None
    return [x.strip() for x in raw.split(",") if x.strip()]


def load_dataset_splits(hf_dataset: str, config: Optional[str] = None):
    from datasets import load_dataset

    if config:
        return load_dataset(hf_dataset, config)
    return load_dataset(hf_dataset)


def resolve_dataset_splits(benchmark: str, spec: Dict[str, Any]):
    errors: List[str] = []
    for dataset_name in spec.get("hf_datasets", []):
        for config_name in spec.get("hf_configs", [None]):
            try:
                dataset = load_dataset_splits(dataset_name, config_name)
                print(
                    f"[OK] benchmark={benchmark} source={dataset_name} "
                    f"config={config_name if config_name is not None else '<none>'}"
                )
                return dataset
            except Exception as exc:  # pylint: disable=broad-except
                errors.append(
                    f"source={dataset_name} config={config_name if config_name is not None else '<none>'}: {exc}"
                )
    joined = "\n  ".join(errors)
    raise RuntimeError(f"Failed to load dataset for benchmark={benchmark}.\n  {joined}")


def first_present(row: Dict[str, Any], keys: Sequence[str]) -> Any:
    for key in keys:
        if key in row and row[key] is not None:
            return row[key]
    return None


def extract_question(row: Dict[str, Any]) -> str:
    question = first_present(row, ["question", "problem", "prompt", "query"])
    if question is None:
        raise ValueError("missing question field")
    q = str(question).strip()
    if not q:
        raise ValueError("empty question")
    return q


def extract_subject(row: Dict[str, Any]) -> str:
    subject = first_present(row, ["subject", "category", "domain", "topic", "field"])
    if subject is None:
        return "unknown"
    return str(subject).strip() or "unknown"


def extract_language(row: Dict[str, Any], benchmark: str) -> str:
    lang = first_present(row, ["language", "lang", "locale"])
    if lang is None:
        return "en" if benchmark in {"mmlu-pro", "global-mmlu", "global-mmlu-lite", "math-500"} else "unknown"
    value = str(lang).strip()
    if value == "":
        return "en" if benchmark in {"mmlu-pro", "global-mmlu", "global-mmlu-lite", "math-500"} else "unknown"
    return value


def extract_id(row: Dict[str, Any], index: int) -> str:
    raw = first_present(row, ["id", "question_id", "qid", "uuid", "unique_id"])
    if raw is None:
        return f"row-{index}"
    out = str(raw).strip()
    if out == "":
        return f"row-{index}"
    return out


def normalize_choice(value: Any) -> str:
    if value is None:
        return ""
    return str(value).strip()


def extract_choices(row: Dict[str, Any]) -> List[str]:
    direct = first_present(row, ["choices", "options", "candidates", "answers"])
    if isinstance(direct, list):
        choices = [normalize_choice(x) for x in direct]
        choices = [x for x in choices if x]
        if len(choices) >= 2:
            return choices
    if isinstance(direct, dict):
        # Accept {"A": "...", "B": "..."} or numeric keys.
        items: List[Tuple[int, str]] = []
        for key, value in direct.items():
            key_str = str(key).strip().upper()
            if key_str in LETTER_TO_INDEX:
                idx = LETTER_TO_INDEX[key_str]
            elif key_str.isdigit():
                idx = int(key_str)
            else:
                continue
            normalized = normalize_choice(value)
            if normalized:
                items.append((idx, normalized))
        items.sort(key=lambda x: x[0])
        if len(items) >= 2:
            return [value for _, value in items]

    letter_map: List[Tuple[int, str]] = []
    for letter, idx in LETTER_TO_INDEX.items():
        if letter in row:
            value = normalize_choice(row[letter])
            if value:
                letter_map.append((idx, value))
    if len(letter_map) >= 2:
        letter_map.sort(key=lambda x: x[0])
        return [value for _, value in letter_map]

    # Handle option_a, option_b, ... style fields
    option_map: List[Tuple[int, str]] = []
    for letter, idx in LETTER_TO_INDEX.items():
        key = f"option_{letter.lower()}"
        if key in row:
            value = normalize_choice(row[key])
            if value:
                option_map.append((idx, value))
    if len(option_map) >= 2:
        option_map.sort(key=lambda x: x[0])
        return [value for _, value in option_map]

    raise ValueError("missing choices/options")


def extract_answer_index(row: Dict[str, Any], choices: List[str]) -> int:
    answer = first_present(
        row,
        [
            "answer_index",
            "answer_idx",
            "answer",
            "label",
            "gold",
            "target",
            "correct_option",
        ],
    )
    if answer is None:
        raise ValueError("missing answer field")

    # Numeric index
    if isinstance(answer, (int, float)):
        idx = int(answer)
        if 0 <= idx < len(choices):
            return idx
        raise ValueError(f"answer index {idx} out of range for {len(choices)} choices")

    answer_str = str(answer).strip()
    if answer_str == "":
        raise ValueError("empty answer")

    # Letter label (A, B, C, ...)
    letter_match = re.fullmatch(r"[A-Za-z]", answer_str)
    if letter_match:
        idx = LETTER_TO_INDEX[answer_str.upper()]
        if idx < len(choices):
            return idx
        raise ValueError(f"answer label {answer_str} out of range")

    # String-encoded integer
    if re.fullmatch(r"-?\d+", answer_str):
        idx = int(answer_str)
        if 0 <= idx < len(choices):
            return idx
        raise ValueError(f"answer index {idx} out of range for {len(choices)} choices")

    # Sometimes answer is the choice text itself.
    normalized = answer_str.lower()
    for idx, choice in enumerate(choices):
        if choice.lower() == normalized:
            return idx

    raise ValueError(f"unrecognized answer value: {answer_str!r}")


def index_to_label(index: int) -> str:
    return chr(ord("A") + index)


def extract_gold_answer(row: Dict[str, Any]) -> str:
    value = first_present(row, ["gold_answer", "answer", "target", "label"])
    if value is None:
        raise ValueError("missing gold answer field")
    answer = str(value).strip()
    if answer == "":
        raise ValueError("empty gold answer")
    return answer


def extract_gold_solution(row: Dict[str, Any]) -> str:
    value = first_present(row, ["gold_solution", "solution", "explanation", "rationale"])
    if value is None:
        return ""
    return str(value).strip()


def normalize_row_mcqa(
    row: Dict[str, Any],
    benchmark: str,
    split: str,
    row_index: int,
) -> Dict[str, Any]:
    question = extract_question(row)
    choices = extract_choices(row)
    answer_index = extract_answer_index(row, choices)
    return {
        "id": extract_id(row, row_index),
        "benchmark": benchmark,
        "split": split,
        "subject": extract_subject(row),
        "language": extract_language(row, benchmark),
        "question": question,
        "choices": choices,
        "answer_index": answer_index,
        "answer_label": index_to_label(answer_index),
    }


def normalize_row_math(
    row: Dict[str, Any],
    benchmark: str,
    split: str,
    row_index: int,
) -> Dict[str, Any]:
    question = extract_question(row)
    gold_answer = extract_gold_answer(row)
    return {
        "id": extract_id(row, row_index),
        "benchmark": benchmark,
        "split": split,
        "subject": extract_subject(row),
        "language": extract_language(row, benchmark),
        "question": question,
        "gold_answer": gold_answer,
        "gold_solution": extract_gold_solution(row),
        # Compatibility field used in admin tables/API payloads.
        "answer_label": gold_answer,
    }


def normalize_row(
    row: Dict[str, Any],
    benchmark: str,
    split: str,
    row_index: int,
) -> Dict[str, Any]:
    if benchmark == "math-500":
        return normalize_row_math(row, benchmark, split, row_index)
    return normalize_row_mcqa(row, benchmark, split, row_index)


def selected_splits(available_splits: Iterable[str], requested: Optional[List[str]]) -> List[str]:
    available = list(available_splits)
    if requested is None:
        return available
    selected = [split for split in requested if split in available]
    if not selected:
        raise ValueError(f"No requested splits found. requested={requested}, available={available}")
    return selected


def write_jsonl(path: str, rows: Iterable[Dict[str, Any]]) -> int:
    os.makedirs(os.path.dirname(path), exist_ok=True)
    count = 0
    with open(path, "w", encoding="utf-8") as f:
        for row in rows:
            f.write(json.dumps(row, ensure_ascii=False) + "\n")
            count += 1
    return count


def normalize_split_rows(
    dataset_split: Iterable[Dict[str, Any]],
    benchmark: str,
    split: str,
    limit: int,
    strict: bool,
) -> Tuple[List[Dict[str, Any]], int]:
    output_rows: List[Dict[str, Any]] = []
    failures = 0
    for i, row in enumerate(dataset_split):
        if limit > 0 and len(output_rows) >= limit:
            break
        try:
            output_rows.append(normalize_row(row, benchmark, split, i))
        except Exception as exc:  # pylint: disable=broad-except
            failures += 1
            if strict:
                raise
            if failures <= 5:
                print(f"[WARN] {benchmark}/{split} row {i} skipped: {exc}")
    return output_rows, failures


def with_split(rows: List[Dict[str, Any]], split: str) -> List[Dict[str, Any]]:
    out: List[Dict[str, Any]] = []
    for row in rows:
        next_row = dict(row)
        next_row["split"] = split
        out.append(next_row)
    return out


def main() -> None:
    args = parse_args()
    benchmarks = parse_benchmarks(args.benchmark)
    requested_splits = parse_splits(args.splits)

    for benchmark in benchmarks:
        spec = BENCHMARK_SPECS[benchmark]
        dataset = resolve_dataset_splits(benchmark, spec)
        if benchmark == "math-500":
            source_split = str(spec.get("source_split", "test"))
            if source_split not in dataset:
                raise RuntimeError(
                    f"math-500 source split {source_split!r} missing in dataset; "
                    f"available={list(dataset.keys())}"
                )
            rows_all, failures = normalize_split_rows(
                dataset[source_split], benchmark, source_split, args.limit, args.strict
            )

            if requested_splits is None:
                requested_math_splits = ["test", "dev"]
            else:
                requested_math_splits = requested_splits

            allowed = {"test", "dev"}
            invalid = [s for s in requested_math_splits if s not in allowed]
            if invalid:
                raise ValueError(
                    f"math-500 supports splits test/dev only; requested invalid={invalid}"
                )

            for split in requested_math_splits:
                if split == "test":
                    rows = with_split(rows_all, "test")
                else:
                    dev_limit = int(spec.get("synthetic_dev_limit", 100))
                    rows = with_split(rows_all[:dev_limit], "dev")

                out_path = os.path.join(args.output_dir, spec["output_dir"], f"{split}.jsonl")
                written = write_jsonl(out_path, rows)
                print(
                    f"[OK] benchmark={benchmark} split={split} "
                    f"written={written} failures={failures} path={out_path}"
                )
            continue

        splits = selected_splits(dataset.keys(), requested_splits)
        for split in splits:
            output_rows, failures = normalize_split_rows(
                dataset[split], benchmark, split, args.limit, args.strict
            )
            out_path = os.path.join(args.output_dir, spec["output_dir"], f"{split}.jsonl")
            written = write_jsonl(out_path, output_rows)
            print(
                f"[OK] benchmark={benchmark} split={split} "
                f"written={written} failures={failures} path={out_path}"
            )


if __name__ == "__main__":
    main()
