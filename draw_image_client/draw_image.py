#!/usr/bin/env python3
"""
Small standalone client for the sub2api/OpenAI-compatible image generation API.

Edit BASE_URL and API_KEY below, then run:
    python draw_image.py "draw a tiny orange cat astronaut"
"""

from __future__ import annotations

import argparse
import base64
import json
import mimetypes
import re
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any


# Built-in configuration. Replace these two values before using the script.
BASE_URL = "https://icode.sampleccx.cn/"
API_KEY = "your-api-key-here"

# Request defaults.
MODEL = "gpt-image-2"
DEFAULT_PROMPT = "draw a tiny orange cat astronaut"
SIZE = "1024x1024"
QUALITY = "high"
N = 1
RESPONSE_FORMAT = "b64_json"
TIMEOUT_SECONDS = 300
OUTPUT_DIR = Path(__file__).resolve().parent / "output"


def main() -> int:
    args = parse_args()
    prompt = " ".join(args.prompt).strip() or DEFAULT_PROMPT

    payload = {
        "model": args.model,
        "prompt": prompt,
        "size": args.size,
        "quality": args.quality,
        "n": args.n,
        "response_format": args.response_format,
    }

    response = post_json(images_url(BASE_URL), payload, API_KEY, TIMEOUT_SECONDS)
    saved_paths = save_images(response, prompt, OUTPUT_DIR)

    print(f"saved {len(saved_paths)} image(s):")
    for path in saved_paths:
        print(path)
    return 0


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Generate images with a built-in API URL and key.")
    parser.add_argument("prompt", nargs="*", help="Image prompt. Uses DEFAULT_PROMPT when omitted.")
    parser.add_argument("--model", default=MODEL, help=f"Image model. Default: {MODEL}")
    parser.add_argument("--size", default=SIZE, help=f"Image size. Default: {SIZE}")
    parser.add_argument("--quality", default=QUALITY, help=f"Image quality. Default: {QUALITY}")
    parser.add_argument("--n", type=int, default=N, help=f"Number of images. Default: {N}")
    parser.add_argument(
        "--response-format",
        choices=("b64_json", "url"),
        default=RESPONSE_FORMAT,
        help=f"Response format. Default: {RESPONSE_FORMAT}",
    )
    return parser.parse_args()


def images_url(base_url: str) -> str:
    clean = base_url.strip().rstrip("/")
    if clean.endswith("/images/generations"):
        return clean
    return f"{clean}/images/generations"


def post_json(url: str, payload: dict[str, Any], api_key: str, timeout: int) -> dict[str, Any]:
    if not api_key or api_key == "replace-with-your-api-key":
        raise RuntimeError("Please edit API_KEY in draw_image.py before running.")

    body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    request = urllib.request.Request(
        url,
        data=body,
        method="POST",
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
            "Accept": "application/json",
        },
    )

    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            response_body = response.read().decode("utf-8")
    except urllib.error.HTTPError as exc:
        error_body = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"HTTP {exc.code}: {error_body}") from exc
    except urllib.error.URLError as exc:
        raise RuntimeError(f"Request failed: {exc.reason}") from exc

    try:
        parsed = json.loads(response_body)
    except json.JSONDecodeError as exc:
        raise RuntimeError(f"Response is not valid JSON: {response_body[:500]}") from exc

    if not isinstance(parsed, dict):
        raise RuntimeError("Response JSON root must be an object.")
    return parsed


def save_images(response: dict[str, Any], prompt: str, output_dir: Path) -> list[Path]:
    data = response.get("data")
    if not isinstance(data, list) or not data:
        raise RuntimeError(f"No image data found in response: {json.dumps(response, ensure_ascii=False)[:500]}")

    output_dir.mkdir(parents=True, exist_ok=True)
    prefix = f"{int(time.time())}_{slug(prompt)}"
    saved_paths: list[Path] = []

    for index, item in enumerate(data, start=1):
        if not isinstance(item, dict):
            continue

        if item.get("b64_json"):
            image_bytes = base64.b64decode(str(item["b64_json"]))
            output_format = str(item.get("output_format") or "png").lower().strip(".")
            path = output_dir / f"{prefix}_{index}.{output_format}"
            path.write_bytes(image_bytes)
            saved_paths.append(path)
            continue

        if item.get("url"):
            url = str(item["url"])
            image_bytes, suffix = download_image(url)
            path = output_dir / f"{prefix}_{index}{suffix}"
            path.write_bytes(image_bytes)
            saved_paths.append(path)

    if not saved_paths:
        raise RuntimeError(f"No b64_json or url image item found: {json.dumps(response, ensure_ascii=False)[:500]}")
    return saved_paths


def download_image(url: str) -> tuple[bytes, str]:
    with urllib.request.urlopen(url, timeout=TIMEOUT_SECONDS) as response:
        body = response.read()
        content_type = response.headers.get("Content-Type", "")

    suffix = mimetypes.guess_extension(content_type.split(";", 1)[0].strip()) or ".png"
    return body, suffix


def slug(text: str, max_length: int = 48) -> str:
    value = re.sub(r"[^a-zA-Z0-9]+", "-", text.lower()).strip("-")
    return (value or "image")[:max_length]


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"error: {exc}", file=sys.stderr)
        raise SystemExit(1)
