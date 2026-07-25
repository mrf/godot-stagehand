#!/usr/bin/env python3
"""Generate a static, dependency-free Go project report card."""

from __future__ import annotations

import argparse
import fnmatch
import html
import json
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
import tomllib
from datetime import UTC, datetime
from pathlib import Path
from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:
    from collections.abc import Callable

CHECK_INFO = {
    "format": (
        "Formatting",
        "Go source files match gofmt's canonical format.",
    ),
    "vet": (
        "Go vet",
        "The standard Go analyzer found no suspicious constructs.",
    ),
    "build": (
        "Build",
        "Every package builds successfully with the project toolchain.",
    ),
    "tests": (
        "Tests & coverage",
        "The test suite passes and reaches the configured coverage target.",
    ),
    "modules": (
        "Module integrity",
        "Downloaded module content matches the hashes in go.sum.",
    ),
}

DEFAULT_WEIGHTS = {
    "format": 15.0,
    "vet": 20.0,
    "build": 20.0,
    "tests": 30.0,
    "modules": 15.0,
}

GRADE_SCALE = (
    (97, "A+"),
    (93, "A"),
    (90, "A-"),
    (87, "B+"),
    (83, "B"),
    (80, "B-"),
    (77, "C+"),
    (73, "C"),
    (70, "C-"),
    (67, "D+"),
    (63, "D"),
    (60, "D-"),
    (0, "F"),
)

MAX_SCORE = 100.0


class ConfigurationError(ValueError):
    """Raised when reportcard/config.toml is invalid."""


def grade_for(score: float) -> str:
    """Return the letter grade for a 0-100 score."""
    return next(grade for threshold, grade in GRADE_SCALE if score >= threshold)


def cap_lines(lines: list[str], limit: int) -> list[str]:
    """Cap a list of lines at `limit`, noting any omission."""
    if len(lines) > limit:
        omitted = len(lines) - limit
        return [*lines[:limit], f"… {omitted} more lines omitted"]
    return lines


def trim_lines(value: str, limit: int | None) -> list[str]:
    """Strip blank lines and cap output at `limit` lines (None = unbounded)."""
    lines = [line.strip() for line in value.splitlines() if line.strip()]
    return lines if limit is None else cap_lines(lines, limit)


def run_command(
    command: list[str], cwd: Path, timeout: int, detail_limit: int | None
) -> tuple[int, list[str], float]:
    """Run a check command and return its exit code, trimmed output, and duration.

    `detail_limit=None` returns every output line uncapped -- needed by callers
    that must locate a specific line (e.g. `go tool cover -func`'s trailing
    `total:` summary) regardless of how many lines precede it; capping first
    and searching after can silently drop the line being searched for.
    """
    started = time.monotonic()
    environment = os.environ.copy()
    environment.update({"NO_COLOR": "1", "TERM": "dumb"})
    try:
        completed = subprocess.run(  # noqa: S603 -- commands are argument arrays from repo-owned config; never a shell
            command,
            cwd=cwd,
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            errors="replace",
            timeout=timeout,
            check=False,
        )
        return (
            completed.returncode,
            trim_lines(completed.stdout, detail_limit),
            time.monotonic() - started,
        )
    except FileNotFoundError:
        return 127, [f"Command not found: {command[0]}"], time.monotonic() - started
    except PermissionError:
        return (
            126,
            [f"Command is not executable: {command[0]}"],
            time.monotonic() - started,
        )
    except subprocess.TimeoutExpired as error:
        captured = error.stdout or ""
        if isinstance(captured, bytes):
            captured = captured.decode(errors="replace")
        capped_limit = None if detail_limit is None else max(0, detail_limit - 1)
        lines = trim_lines(captured, capped_limit)
        lines.append(f"Timed out after {timeout} seconds")
        return 124, lines, time.monotonic() - started


def result(  # noqa: PLR0913 -- a report row is nine independent scalar fields; callers (including tests) rely on this flat signature
    check_id: str,
    label: str,
    description: str,
    weight: float,
    score: float,
    observed: str,
    details: list[str],
    duration: float,
    status: str | None = None,
) -> dict[str, Any]:
    """Build one check-result row for the report, bounding the score to 0-100."""
    bounded = round(max(0.0, min(MAX_SCORE, score)), 1)
    if status is None:
        status = (
            "passed" if bounded >= MAX_SCORE else "warning" if bounded > 0 else "failed"
        )
    return {
        "id": check_id,
        "label": label,
        "description": description,
        "weight": weight,
        "score": bounded,
        "observed": observed,
        "status": status,
        "details": details,
        "duration_seconds": round(duration, 2),
    }


def command_check(  # noqa: PLR0913 -- mirrors result()'s flat field style; every argument is a distinct check input
    check_id: str,
    command: list[str],
    source: Path,
    weight: float,
    timeout: int,
    detail_limit: int,
) -> dict[str, Any]:
    """Score a built-in check as pass/fail from a single command's exit code."""
    label, description = CHECK_INFO[check_id]
    code, details, duration = run_command(command, source, timeout, detail_limit)
    return result(
        check_id,
        label,
        description,
        weight,
        100 if code == 0 else 0,
        "Clean" if code == 0 else f"Exited {code}",
        [] if code == 0 else details,
        duration,
    )


def format_check(
    source: Path,
    weight: float,
    timeout: int,
    detail_limit: int,
    excludes: list[str],
) -> dict[str, Any]:
    """Score the share of Go files that gofmt accepts as canonically formatted."""
    label, description = CHECK_INFO["format"]
    files = []
    for path in source.rglob("*.go"):
        if not path.is_file():
            continue
        relative = path.relative_to(source).as_posix()
        if not any(fnmatch.fnmatch(relative, pattern) for pattern in excludes):
            files.append(relative)
    files.sort()
    if not files:
        return result(
            "format",
            label,
            description,
            weight,
            0,
            "No Go files",
            ["No Go files matched the configured source directory."],
            0,
        )

    started = time.monotonic()
    unformatted: list[str] = []
    errors: list[str] = []
    for offset in range(0, len(files), 100):
        code, output, _ = run_command(
            ["gofmt", "-l", *files[offset : offset + 100]],
            source,
            timeout,
            detail_limit,
        )
        if code != 0:
            errors.extend(output)
        else:
            unformatted.extend(output)
    duration = time.monotonic() - started
    if errors:
        return result(
            "format",
            label,
            description,
            weight,
            0,
            "Unable to check",
            errors[:detail_limit],
            duration,
        )
    formatted = len(files) - len(unformatted)
    score = formatted / len(files) * 100
    observed = f"{formatted} / {len(files)} files formatted"
    return result(
        "format",
        label,
        description,
        weight,
        score,
        observed,
        unformatted[:detail_limit],
        duration,
    )


def tests_check(  # noqa: PLR0913 -- one argument per independently-configured input; splitting would just move the list into a struct
    source: Path,
    weight: float,
    timeout: int,
    detail_limit: int,
    coverage_target: float,
    extra_args: list[str],
) -> dict[str, Any]:
    """Run the Go test suite and score coverage against the configured target.

    `coverage_percent` is added to the returned row (null when coverage could
    not be measured) so callers get the raw number without parsing `observed`.
    """
    label, description = CHECK_INFO["tests"]
    with tempfile.TemporaryDirectory(prefix="go-ci-report-card-") as temp_dir:
        profile = Path(temp_dir) / "coverage.out"
        code, details, duration = run_command(
            [
                "go",
                "test",
                *extra_args,
                "-covermode=atomic",
                f"-coverprofile={profile}",
                "./...",
            ],
            source,
            timeout,
            detail_limit,
        )

        def unmeasured(observed: str, unmeasured_details: list[str]) -> dict[str, Any]:
            row = result(
                "tests",
                label,
                description,
                weight,
                0,
                observed,
                cap_lines(unmeasured_details, detail_limit),
                duration,
            )
            row["coverage_percent"] = None
            return row

        if code != 0:
            return unmeasured("Tests failed", details)
        # Uncapped: the `total:` summary is the last line, and a project with
        # enough functions to exceed detail_limit would otherwise have it cut
        # off before the search below ever sees it.
        cover_code, cover_output, cover_duration = run_command(
            ["go", "tool", "cover", f"-func={profile}"],
            source,
            timeout,
            None,
        )
        duration += cover_duration
        if cover_code != 0:
            return unmeasured("Coverage unavailable", cover_output)
        match = next(
            (
                re.search(r"total:\s+\(statements\)\s+([0-9.]+)%", line)
                for line in cover_output
                if line.startswith("total:")
            ),
            None,
        )
        if match is None:
            return unmeasured("Coverage unavailable", cover_output)
        coverage = float(match.group(1))
        score = (
            100 if coverage_target <= 0 else min(100, coverage / coverage_target * 100)
        )
        status = (
            "passed"
            if coverage >= coverage_target
            else "warning"
            if coverage > 0
            else "failed"
        )
        row = result(
            "tests",
            label,
            description,
            weight,
            score,
            f"{coverage:.1f}% / {coverage_target:g}% target",
            []
            if status == "passed"
            else [f"Coverage is {coverage_target - coverage:.1f} points below target."],
            duration,
            status,
        )
        row["coverage_percent"] = coverage
        return row


def custom_check(
    config: dict[str, Any],
    source: Path,
    timeout: int,
    detail_limit: int,
) -> dict[str, Any]:
    """Validate one [[custom_checks]] table and score its command pass/fail."""
    check_id = str(config.get("id", "")).strip()
    label = str(config.get("label", check_id)).strip()
    description = str(
        config.get("description", "Project-specific command completes successfully.")
    ).strip()
    command = config.get("command")
    weight = float(config.get("weight", 0))
    if not check_id or not re.fullmatch(r"[a-z0-9_-]+", check_id):
        raise ConfigurationError(
            "Every custom check needs a lowercase id using letters, numbers, _ or -."
        )
    if not label:
        raise ConfigurationError(f"Custom check {check_id!r} needs a label.")
    if (
        not isinstance(command, list)
        or not command
        or not all(isinstance(part, str) for part in command)
    ):
        raise ConfigurationError(
            f"Custom check {check_id!r} command must be a non-empty string array."
        )
    if weight <= 0:
        raise ConfigurationError(
            f"Custom check {check_id!r} weight must be greater than zero."
        )
    code, details, duration = run_command(command, source, timeout, detail_limit)
    return result(
        check_id,
        label,
        description,
        weight,
        100 if code == 0 else 0,
        "Clean" if code == 0 else f"Exited {code}",
        [] if code == 0 else details,
        duration,
    )


def git_value(repo_root: Path, *arguments: str) -> str:
    """Return trimmed git output for repo metadata, or "" if git is unavailable."""
    try:
        completed = subprocess.run(  # noqa: S603 -- fixed git subcommands with no user-controlled arguments
            ["git", *arguments],  # noqa: S607 -- git is resolved from PATH on purpose; its location varies by runner
            cwd=repo_root,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
            timeout=5,
            check=False,
        )
        return completed.stdout.strip() if completed.returncode == 0 else ""
    except (FileNotFoundError, subprocess.TimeoutExpired):
        return ""


def load_configuration(path: Path) -> dict[str, Any]:
    """Load config.toml and validate that its top-level sections are tables."""
    try:
        with path.open("rb") as handle:
            config = tomllib.load(handle)
    except FileNotFoundError as error:
        raise ConfigurationError(f"Configuration file not found: {path}") from error
    except tomllib.TOMLDecodeError as error:
        raise ConfigurationError(f"Invalid TOML in {path}: {error}") from error
    if not isinstance(config.get("project", {}), dict):
        raise ConfigurationError("[project] must be a table.")
    if not isinstance(config.get("quality", {}), dict):
        raise ConfigurationError("[quality] must be a table.")
    if not isinstance(config.get("checks", {}), dict):
        raise ConfigurationError("[checks] must be a table.")
    return config


def _custom_checks(
    config: dict[str, Any],
    source: Path,
    timeout: int,
    detail_limit: int,
) -> list[dict[str, Any]]:
    """Validate and run every enabled [[custom_checks]] entry."""
    custom_configs = config.get("custom_checks", [])
    if not isinstance(custom_configs, list):
        raise ConfigurationError(
            "[[custom_checks]] entries must be an array of tables."
        )
    checks: list[dict[str, Any]] = []
    for check_config in custom_configs:
        if not isinstance(check_config, dict):
            raise ConfigurationError("Every custom check must be a table.")
        if check_config.get("enabled", True):
            checks.append(custom_check(check_config, source, timeout, detail_limit))
    return checks


def _tests_extra_args(checks_config: dict[str, Any]) -> list[str]:
    """Validate and return checks.tests.extra_args, e.g. build tags like -tags=godot."""
    extra_args = checks_config.get("tests", {}).get("extra_args", [])
    if not isinstance(extra_args, list) or not all(
        isinstance(item, str) for item in extra_args
    ):
        raise ConfigurationError("checks.tests.extra_args must be an array of strings.")
    return extra_args


def _build_checks(
    config: dict[str, Any],
    source: Path,
    excludes: list[str],
    timeout: int,
    detail_limit: int,
) -> list[dict[str, Any]]:
    """Run every enabled built-in and custom check and collect their result rows."""
    checks_config = config.get("checks", {})
    runners: dict[str, Callable[[float], dict[str, Any]]] = {
        "format": lambda weight: format_check(
            source, weight, timeout, detail_limit, excludes
        ),
        "vet": lambda weight: command_check(
            "vet", ["go", "vet", "./..."], source, weight, timeout, detail_limit
        ),
        "build": lambda weight: command_check(
            "build", ["go", "build", "./..."], source, weight, timeout, detail_limit
        ),
        "tests": lambda weight: tests_check(
            source,
            weight,
            timeout,
            detail_limit,
            float(checks_config.get("tests", {}).get("coverage_target", 70)),
            _tests_extra_args(checks_config),
        ),
        "modules": lambda weight: command_check(
            "modules", ["go", "mod", "verify"], source, weight, timeout, detail_limit
        ),
    }

    checks: list[dict[str, Any]] = []
    for check_id in ("format", "vet", "build", "tests", "modules"):
        check_config = checks_config.get(check_id, {})
        if not isinstance(check_config, dict):
            raise ConfigurationError(f"[checks.{check_id}] must be a table.")
        if check_config.get("enabled", True):
            weight = float(check_config.get("weight", DEFAULT_WEIGHTS[check_id]))
            if weight <= 0:
                raise ConfigurationError(
                    f"checks.{check_id}.weight must be greater than zero."
                )
            checks.append(runners[check_id](weight))

    checks.extend(_custom_checks(config, source, timeout, detail_limit))

    if not checks:
        raise ConfigurationError("At least one check must be enabled.")
    ids = [check["id"] for check in checks]
    if len(ids) != len(set(ids)):
        raise ConfigurationError("Check ids must be unique.")
    return checks


def _repository_metadata(
    project: dict[str, Any], repo_root: Path, source: Path
) -> tuple[dict[str, str], str]:
    """Resolve the repository identity and project name from env vars or git."""
    slug = os.getenv("GITHUB_REPOSITORY", "")
    repository_url = str(project.get("repository_url", "")).strip()
    if not repository_url and slug:
        repository_url = f"https://github.com/{slug}"
    if repository_url and not re.fullmatch(r"https?://[^\s]+", repository_url):
        raise ConfigurationError(
            "project.repository_url must be an http:// or https:// URL."
        )
    commit = os.getenv("GITHUB_SHA", "") or git_value(repo_root, "rev-parse", "HEAD")
    branch = os.getenv("GITHUB_REF_NAME", "") or git_value(
        repo_root, "branch", "--show-current"
    )
    name = str(project.get("name", "")).strip() or (
        slug.rsplit("/", 1)[-1] if slug else source.name
    )
    repository = {
        "slug": slug,
        "url": repository_url,
        "branch": branch,
        "sha": commit,
        "short_sha": commit[:8],
    }
    return repository, name


def make_report(config: dict[str, Any], repo_root: Path) -> dict[str, Any]:
    """Run the configured checks and assemble the weighted, graded report."""
    project = config.get("project", {})
    quality = config.get("quality", {})
    source = (repo_root / str(project.get("source_dir", "."))).resolve()
    if not source.is_dir():
        raise ConfigurationError(f"project.source_dir is not a directory: {source}")
    try:
        source.relative_to(repo_root.resolve())
    except ValueError as error:
        raise ConfigurationError(
            "project.source_dir must stay inside the repository."
        ) from error

    minimum_score = float(quality.get("minimum_score", 80))
    detail_limit = int(quality.get("details_limit", 80))
    timeout = int(quality.get("command_timeout_seconds", 300))
    if not 0 <= minimum_score <= MAX_SCORE:
        raise ConfigurationError("quality.minimum_score must be between 0 and 100.")
    if detail_limit < 1 or timeout < 1:
        raise ConfigurationError(
            "quality details_limit and command_timeout_seconds must be positive."
        )

    excludes = project.get("exclude", [])
    if not isinstance(excludes, list) or not all(
        isinstance(item, str) for item in excludes
    ):
        raise ConfigurationError("project.exclude must be an array of glob strings.")

    checks = _build_checks(config, source, excludes, timeout, detail_limit)
    total_weight = sum(check["weight"] for check in checks)
    score = sum(check["score"] * check["weight"] for check in checks) / total_weight
    score = round(score, 1)

    repository, name = _repository_metadata(project, repo_root, source)
    now = os.getenv("REPORTCARD_NOW", "") or datetime.now(UTC).isoformat().replace(
        "+00:00", "Z"
    )
    tests_check_row = next((c for c in checks if c["id"] == "tests"), None)
    coverage_percent = (
        tests_check_row.get("coverage_percent") if tests_check_row else None
    )

    return {
        "schema_version": 1,
        "project": {
            "name": name,
            "tagline": str(project.get("tagline", "Automated Go quality report.")),
            "accent": str(project.get("accent", "#e4572e")),
        },
        "repository": repository,
        "generated_at": now,
        "minimum_score": minimum_score,
        "score": score,
        "grade": grade_for(score),
        "passed": score >= minimum_score,
        # Raw tests-check coverage, duplicated at the top level so downstream
        # consumers (e.g. a README badge workflow) don't have to find and
        # parse the "tests" row's `observed` string. Null if coverage was
        # unmeasurable (tests failed, or the tests check is disabled).
        "coverage_percent": coverage_percent,
        "checks": checks,
    }


def validate_accent(value: str) -> str:
    """Return the accent color if it is a six-digit hex code, else the default."""
    return value if re.fullmatch(r"#[0-9a-fA-F]{6}", value) else "#e4572e"


def write_site(report: dict[str, Any], output: Path, package_dir: Path) -> None:
    """Write the static site: index.html, report.json, assets, and .nojekyll."""
    assets_output = output / "assets"
    assets_output.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(package_dir / "assets" / "style.css", assets_output / "style.css")
    shutil.copyfile(package_dir / "assets" / "app.js", assets_output / "app.js")

    report_json = json.dumps(report, ensure_ascii=False, separators=(",", ":"))
    safe_embedded_json = report_json.replace("</", "<\\/")
    title = html.escape(report["project"]["name"], quote=True)
    description = html.escape(report["project"]["tagline"], quote=True)
    accent = validate_accent(report["project"]["accent"])
    template = (package_dir / "template.html").read_text(encoding="utf-8")
    page = (
        template.replace("__TITLE__", title)
        .replace("__DESCRIPTION__", description)
        .replace("__ACCENT__", accent)
        .replace("__REPORT_DATA__", safe_embedded_json)
    )
    (output / "index.html").write_text(page, encoding="utf-8")
    (output / "report.json").write_text(
        json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    (output / ".nojekyll").write_text("", encoding="utf-8")


def write_ci_metadata(report: dict[str, Any], github_output: str | None) -> None:
    """Publish gate outputs and a summary table to the GitHub Actions files."""
    if github_output:
        coverage_percent = report.get("coverage_percent")
        with Path(github_output).open("a", encoding="utf-8") as handle:
            handle.write(f"passed={str(report['passed']).lower()}\n")
            handle.write(f"score={report['score']}\n")
            handle.write(f"grade={report['grade']}\n")
            handle.write(
                f"coverage_percent={'' if coverage_percent is None else coverage_percent}\n"
            )
    step_summary = os.getenv("GITHUB_STEP_SUMMARY")
    if step_summary:
        rows = "\n".join(
            f"| {check['label']} | {check['observed']} | {check['score']:.1f} |"
            for check in report["checks"]
        )
        outcome = "Passed" if report["passed"] else "Needs attention"
        with Path(step_summary).open("a", encoding="utf-8") as handle:
            handle.write(
                f"## Go CI Report Card: {report['grade']} ({report['score']:.1f})\n\n"
                f"**Quality gate:** {outcome} (minimum {report['minimum_score']})\n\n"
                "| Check | Result | Score |\n|---|---:|---:|\n"
                f"{rows}\n"
            )


def parse_args() -> argparse.Namespace:
    """Parse the generator's command-line arguments."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, default=Path("reportcard/config.toml"))
    parser.add_argument("--repo-root", type=Path, default=Path())
    parser.add_argument("--output", type=Path, default=Path("_site"))
    parser.add_argument(
        "--github-output",
        help="Path supplied by the GITHUB_OUTPUT environment variable",
    )
    parser.add_argument(
        "--enforce", action="store_true", help="Exit 1 when the quality gate is missed"
    )
    return parser.parse_args()


def main() -> int:
    """Generate the report site and return the process exit code."""
    args = parse_args()
    repo_root = args.repo_root.resolve()
    package_dir = Path(__file__).resolve().parent
    config_path = args.config if args.config.is_absolute() else repo_root / args.config
    output = args.output if args.output.is_absolute() else repo_root / args.output
    try:
        config = load_configuration(config_path)
        report = make_report(config, repo_root)
        write_site(report, output, package_dir)
        write_ci_metadata(report, args.github_output)
    except ConfigurationError as error:
        print(f"configuration error: {error}", file=sys.stderr)
        return 2
    except OSError as error:
        print(f"generation error: {error}", file=sys.stderr)
        return 2

    outcome = "PASS" if report["passed"] else "FAIL"
    print(f"{outcome}  Grade {report['grade']}  Score {report['score']:.1f}/100")
    print(f"Static report written to {output}")
    return 1 if args.enforce and not report["passed"] else 0


if __name__ == "__main__":
    raise SystemExit(main())
