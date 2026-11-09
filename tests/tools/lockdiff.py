"""Compare two uv lock files package by package (stdlib only).

    python3 -I tests/tools/lockdiff.py OURS THEIRS

Every package of OURS (except a virtual/editable project row) must exist in THEIRS with the same
version and the identical set of sdist and wheel sha256 hashes. Prints a JSON report on stdout:

    {"checked": N, "missing": [...], "mismatched": [...]}

Exit 0 when everything is identical, 1 when any package is missing or mismatched, 2 when an input
cannot be read or parsed. This is the automated replacement for a human package-legitimacy review
of the Python test dependencies: the product repo's lock is the reviewed reference.
"""

import json
import sys
import tomllib


def load(path):
    with open(path, "rb") as f:
        return tomllib.load(f)


def artifacts(pkg):
    """The set of sha256 hashes of a package's sdist and wheels."""
    hashes = set()
    sdist = pkg.get("sdist")
    if sdist and "hash" in sdist:
        hashes.add(sdist["hash"])
    for wheel in pkg.get("wheels", []):
        if "hash" in wheel:
            hashes.add(wheel["hash"])
    return hashes


def is_project_row(pkg):
    src = pkg.get("source", {})
    return "virtual" in src or "editable" in src


def compare(ours, theirs):
    ref = {}
    for pkg in theirs.get("package", []):
        ref.setdefault(pkg["name"], []).append(pkg)
    checked, missing, mismatched = 0, [], []
    for pkg in ours.get("package", []):
        if is_project_row(pkg):
            continue
        checked += 1
        name, version = pkg["name"], pkg.get("version")
        cands = ref.get(name)
        if not cands:
            missing.append({"name": name, "version": version})
            continue
        want = artifacts(pkg)
        match = [c for c in cands if c.get("version") == version and artifacts(c) == want]
        if match and want:
            continue
        theirs_c = cands[0]
        mismatched.append(
            {
                "name": name,
                "version": version,
                "reference_version": theirs_c.get("version"),
                "only_ours": sorted(want - artifacts(theirs_c)),
                "only_reference": sorted(artifacts(theirs_c) - want),
                "no_hashes": not want,
            }
        )
    return {"checked": checked, "missing": missing, "mismatched": mismatched}


def main(argv):
    if len(argv) != 3:
        print("usage: lockdiff.py OURS THEIRS", file=sys.stderr)
        return 2
    try:
        ours, theirs = load(argv[1]), load(argv[2])
    except (OSError, tomllib.TOMLDecodeError) as exc:
        print(json.dumps({"error": f"{type(exc).__name__}: {exc}"}))
        return 2
    report = compare(ours, theirs)
    print(json.dumps(report, indent=2))
    return 0 if not report["missing"] and not report["mismatched"] else 1


if __name__ == "__main__":
    sys.exit(main(sys.argv))
