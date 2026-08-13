#!/usr/bin/env python3
"""Generate a small deterministic SPDX SBOM from the Go module graph."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
from datetime import datetime, timezone
from pathlib import Path

from generate_third_party_notices import license_for, modules


def package_id(path: str, version: str) -> str:
    digest = hashlib.sha256(f"{path}@{version}".encode()).hexdigest()[:16]
    return f"SPDXRef-Package-{digest}"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--version", default=os.environ.get("VERSION_BUILD", "dev"))
    args = parser.parse_args()
    items = modules()
    epoch = int(os.environ.get("SOURCE_DATE_EPOCH", "0"))
    created = datetime.fromtimestamp(epoch, timezone.utc).isoformat().replace("+00:00", "Z")
    root = "SPDXRef-DOCUMENT"
    application = "SPDXRef-Package-biofetch"
    packages = [
        {
            "SPDXID": application,
            "name": "github.com/FuqingZh/biofetch",
            "versionInfo": args.version,
            "downloadLocation": "https://github.com/FuqingZh/biofetch",
            "licenseConcluded": "Apache-2.0",
            "licenseDeclared": "Apache-2.0",
            "copyrightText": "NOASSERTION",
            "filesAnalyzed": False,
        }
    ]
    relationships = [
        {"spdxElementId": root, "relationshipType": "DESCRIBES", "relatedSpdxElement": application}
    ]
    for item in items:
        ident = package_id(item["path"], item["version"])
        packages.append(
            {
                "SPDXID": ident,
                "name": item["path"],
                "versionInfo": item["version"],
                "downloadLocation": f"https://pkg.go.dev/{item['path']}@{item['version']}",
                "licenseConcluded": license_for(item["path"]),
                "licenseDeclared": license_for(item["path"]),
                "copyrightText": "NOASSERTION",
                "filesAnalyzed": False,
            }
        )
        relationships.append(
            {"spdxElementId": application, "relationshipType": "DEPENDS_ON", "relatedSpdxElement": ident}
        )
    document = {
        "spdxVersion": "SPDX-2.3",
        "dataLicense": "CC0-1.0",
        "SPDXID": root,
        "name": f"biofetch-{args.version}",
        "documentNamespace": f"https://github.com/FuqingZh/biofetch/spdx/{args.version}",
        "creationInfo": {"created": created, "creators": ["Tool: biofetch/scripts/generate_sbom.py"]},
        "packages": packages,
        "relationships": relationships,
    }
    args.output.write_text(json.dumps(document, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
