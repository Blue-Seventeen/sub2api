#!/usr/bin/env python3
"""Record the routing golden from the pinned main source, never the checkout."""

import argparse
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile


def format_golden(fixture):
    """Keep observations readable and wrap case indexes in compact rows."""
    metadata = {key: value for key, value in fixture.items() if key != "results"}
    prefix = json.dumps(metadata, ensure_ascii=False, indent=2)[:-2]
    indexes = fixture["results"]
    rows = ["    " + ", ".join(map(str, indexes[i:i + 32])) for i in range(0, len(indexes), 32)]
    return (prefix + ',\n  "results": [\n' + ",\n".join(rows) + "\n  ]\n}\n").encode("utf-8")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="Compare the recorded fixture without overwriting it")
    args = parser.parse_args()
    repo = Path(__file__).resolve().parents[4]
    driver = repo / "backend/internal/service/upstream_protocol_routing_matrix_test.go"
    source = driver.read_text(encoding="utf-8")
    revision = re.search(r'const routingMatrixMainRevision = "([0-9a-f]{40})"', source).group(1)
    subprocess.run(["git", "cat-file", "-e", revision + "^{commit}"], cwd=repo, check=True)
    archive = subprocess.check_output(["git", "archive", revision, "backend"], cwd=repo)

    # The checkout moved this unused production helper into the test driver.
    # The pinned main revision still defines it in opencode_go.go; keep using
    # that original implementation when recording, without a duplicate symbol.
    source, removed = re.subn(
        r"(?ms)^// openCodeGoNativeProtocol .*?^}\n\n", "", source, count=1,
    )
    if removed != 1:
        raise RuntimeError("Expected the checkout-only OpenCode test helper")

    # This checkout-only comparison calls a newer routing helper absent from
    # pinned main. Keep it in the normal suite, but not the recording driver:
    # recording only observes main's real gateways and captures their results.
    source, removed = re.subn(
        r"(?ms)^func TestUpstreamProtocolRoutingMatrixMatchesLegacy\(t \*testing.T\) \{.*?^}\n\n",
        "", source, count=1,
    )
    if removed != 1:
        raise RuntimeError("Expected the checkout-only legacy routing comparison")
    source, removed = re.subn(r'^\s*"sort"\n', "", source, count=1, flags=re.M)
    if removed != 1:
        raise RuntimeError("Expected the removed comparison's sort import")

    with tempfile.TemporaryDirectory(prefix="sub2api-routing-main-") as scratch:
        root = Path(scratch)
        with tarfile.open(fileobj=io.BytesIO(archive)) as files:
            files.extractall(root, filter="data")
        recording_driver = root / "backend/internal/service/upstream_protocol_routing_matrix_test.go"
        recording_driver.write_text(source + r'''

// This helper is compiled only by the golden-recording tool, not by normal tests.
func captureRoutingMatrixGolden() routingMatrixGolden {
    cases := routingMatrixMainCases()
    golden := routingMatrixGolden{SourceRevision: routingMatrixMainRevision, CaseCount: len(cases), CaseKeysSHA256: routingMatrixCaseDigest(cases)}
    indexByObservation := make(map[string]int)
    for _, tc := range cases {
        obs := observeRouting(tc)
        key := obs.String()
        index, ok := indexByObservation[key]
        if !ok {
            index = len(golden.Observations)
            golden.Observations = append(golden.Observations, obs)
            indexByObservation[key] = index
        }
        golden.Results = append(golden.Results, index)
    }
    return golden
}

func TestRecordUpstreamProtocolRoutingMainGolden(t *testing.T) {
    gin.SetMode(gin.TestMode)
    data, err := json.MarshalIndent(captureRoutingMatrixGolden(), "", "  ")
    require.NoError(t, err)
    require.NoError(t, os.WriteFile(os.Getenv("ROUTING_MATRIX_RECORD_OUTPUT"), append(data, '\n'), 0o644))
}
''', encoding="utf-8")
        output = root / "routing_main_golden.json"
        environment = dict(os.environ, ROUTING_MATRIX_RECORD_OUTPUT=str(output))
        subprocess.run(
            ["go", "test", "-p", "1", "-tags", "unit", "./internal/service/", "-run", "^TestRecordUpstreamProtocolRoutingMainGolden$", "-count=1", "-timeout=15m"],
            cwd=root / "backend", env=environment, check=True,
        )
        data = output.read_bytes()
        fixture = json.loads(data)
        if fixture["source_revision"] != revision:
            raise RuntimeError("Recorded source revision differs from the pinned revision")
        destination = driver.parent / "testdata/upstream_protocol_routing_main_golden.json"
        recorded = format_golden(fixture)
        if args.check:
            if destination.read_bytes() != recorded:
                raise RuntimeError("Recorded fixture differs from the checked-in golden")
            print(f"Verified {fixture['case_count']} cases from {revision}; golden unchanged")
            return
        destination.write_bytes(recorded)
        print(f"Recorded {fixture['case_count']} cases from {revision} into {destination}")


if __name__ == "__main__":
    main()
