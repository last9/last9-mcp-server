#!/usr/bin/env python3
"""Refresh pinned OTel metric inputs (requires PyYAML), then regenerate the lookup."""
import hashlib
import json
from pathlib import Path
import subprocess
import urllib.request

import yaml

VERSION = "v0.120.0"
RECEIVERS = [
    "kubeletstats", "k8scluster", "dockerstats", "mongodb", "oracledb",
    "nginx", "kafkametrics", "sqlserver", "postgresql", "redis", "mysql",
    "apache", "iis",
]
SCRAPERS = ["cpu", "disk", "filesystem", "load", "memory", "network", "paging", "process", "processes"]
ROOT = Path(__file__).resolve().parents[1]


def main():
    paths = [f"receiver/{name}receiver/metadata.yaml" for name in RECEIVERS]
    paths += [f"receiver/hostmetricsreceiver/internal/scraper/{name}scraper/metadata.yaml" for name in SCRAPERS]
    sources = []
    for path in sorted(paths):
        url = f"https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/{VERSION}/{path}"
        with urllib.request.urlopen(url, timeout=30) as response:
            raw = response.read(1024 * 1024 + 1)
        if len(raw) > 1024 * 1024:
            raise ValueError(f"oversized metadata: {path}")
        metrics = yaml.safe_load(raw)["metrics"]
        entries = []
        for name, metric in sorted(metrics.items()):
            kinds = [kind for kind in ("sum", "gauge", "histogram", "exponential_histogram") if kind in metric]
            if len(kinds) != 1:
                raise ValueError(f"unknown metric kind: {name}")
            kind = kinds[0]
            entries.append(dict(name=name, kind=kind, monotonic=metric[kind].get("monotonic", False),
                                unit=str(metric["unit"]), help=metric["description"]))
        sources.append(dict(path=path, version=VERSION, sha256=hashlib.sha256(raw).hexdigest(), metrics=entries))
    target = ROOT / "internal/metriccatalog/receiver_sources.json"
    target.write_text(json.dumps(sources, indent=2, ensure_ascii=False) + "\n")
    subprocess.run(["go", "run", "./internal/metriccatalog/cmd/generate"], cwd=ROOT, check=True)


if __name__ == "__main__":
    main()
