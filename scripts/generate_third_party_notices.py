#!/usr/bin/env python3
"""Render the dependency-license inventory for a tagged biofetch release."""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path


EXACT_LICENSES = {
    "github.com/aymanbagabas/go-osc52/v2": "MIT",
    "github.com/aymanbagabas/go-udiff": "MIT",
    "github.com/charmbracelet/colorprofile": "MIT",
    "github.com/charmbracelet/lipgloss": "MIT",
    "github.com/charmbracelet/log": "MIT",
    "github.com/charmbracelet/x/ansi": "MIT",
    "github.com/charmbracelet/x/cellbuf": "MIT",
    "github.com/charmbracelet/x/exp/golden": "MIT",
    "github.com/charmbracelet/x/term": "MIT",
    "github.com/cpuguy83/go-md2man/v2": "MIT",
    "github.com/davecgh/go-spew": "ISC",
    "github.com/go-logfmt/logfmt": "MIT",
    "github.com/google/go-cmp": "BSD-3-Clause",
    "github.com/inconshreveable/mousetrap": "MIT",
    "github.com/lucasb-eyer/go-colorful": "MIT",
    "github.com/mattn/go-isatty": "MIT",
    "github.com/mattn/go-runewidth": "MIT",
    "github.com/muesli/termenv": "MIT",
    "github.com/pelletier/go-toml/v2": "MIT",
    "github.com/pmezard/go-difflib": "BSD-3-Clause",
    "github.com/rivo/uniseg": "MIT",
    "github.com/russross/blackfriday/v2": "BSD-2-Clause",
    "github.com/spf13/cobra": "Apache-2.0",
    "github.com/spf13/pflag": "BSD-3-Clause",
    "github.com/stretchr/testify": "MIT",
    "github.com/ulikunitz/xz": "BSD-3-Clause",
    "github.com/xo/terminfo": "MIT",
    "go.yaml.in/yaml/v3": "MIT",
    "gopkg.in/check.v1": "BSD-2-Clause",
    "gopkg.in/yaml.v3": "MIT",
}


def license_for(path: str) -> str | None:
    if path in EXACT_LICENSES:
        return EXACT_LICENSES[path]
    if path.startswith("golang.org/x/"):
        return "BSD-3-Clause"
    return None


def modules() -> list[dict[str, str]]:
    output = subprocess.check_output(["go", "list", "-m", "-json", "all"], text=True)
    decoder = json.JSONDecoder()
    result: list[dict[str, str]] = []
    offset = 0
    while offset < len(output):
        while offset < len(output) and output[offset].isspace():
            offset += 1
        if offset >= len(output):
            break
        item, end = decoder.raw_decode(output, offset)
        offset = end
        path = item.get("Path")
        version = item.get("Version", "")
        if path and version:
            result.append({"path": path, "version": version})
    return sorted(result, key=lambda item: item["path"])


def render(items: list[dict[str, str]]) -> str:
    unknown = [item["path"] for item in items if license_for(item["path"]) is None]
    if unknown:
        joined = ", ".join(unknown)
        raise SystemExit(
            "unclassified dependency license(s): "
            f"{joined}; review and extend EXACT_LICENSES before release"
        )

    lines = [
        "# Third-party dependency notices",
        "",
        "This file is generated from the exact `go list -m -json all` graph.",
        "Run `python3 scripts/generate_third_party_notices.py --check` after",
        "changing `go.mod` or `go.sum`. License identifiers are SPDX identifiers;",
        "the corresponding source distribution remains the authoritative text.",
        "",
        "| Module | Version | License | Source |",
        "| --- | --- | --- | --- |",
    ]
    for item in items:
        path = item["path"]
        version = item["version"]
        source = f"https://pkg.go.dev/{path}@{version}"
        lines.append(f"| `{path}` | `{version}` | `{license_for(path)}` | {source} |")
    lines.extend(
        [
            "",
            "The Apache-2.0 license in `LICENSE` applies only to repository-owned",
            "biofetch code and documentation. It does not relicense upstream",
            "database snapshots downloaded by the CLI.",
            "",
        ]
    )
    return "\n".join(lines)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true")
    parser.add_argument("--output", type=Path, default=Path("THIRD_PARTY_NOTICES"))
    args = parser.parse_args()
    generated = render(modules())
    if args.check:
        expected = args.output.read_text(encoding="utf-8") if args.output.exists() else ""
        if expected != generated:
            print(f"{args.output} is out of date", file=sys.stderr)
            return 1
        return 0
    args.output.write_text(generated, encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
