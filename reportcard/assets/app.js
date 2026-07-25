(() => {
  "use strict";

  const report = JSON.parse(document.getElementById("report-data").textContent);
  const byId = (id) => document.getElementById(id);
  const make = (tag, className, text) => {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  };

  const statusLabel = {
    passed: "Passed",
    warning: "Partial",
    failed: "Failed",
    skipped: "Skipped",
  };

  byId("project-name").textContent = report.project.name;
  byId("tagline").textContent = report.project.tagline;
  byId("grade").textContent = report.grade;
  byId("score").textContent = `${report.score.toFixed(1)} / 100`;
  byId("grade-card").dataset.grade = report.grade.charAt(0).toLowerCase();
  byId("gate").textContent = report.passed
    ? `Quality gate passed · target ${report.minimum_score}`
    : `Quality gate missed · target ${report.minimum_score}`;
  byId("gate").classList.add(report.passed ? "gate-pass" : "gate-fail");

  const repositoryLink = byId("repo-link");
  if (report.repository.url) {
    repositoryLink.href = report.repository.url;
    repositoryLink.hidden = false;
  }

  const meta = byId("repo-meta");
  [report.repository.slug, report.repository.branch, report.repository.short_sha]
    .filter(Boolean)
    .forEach((value) => meta.append(make("code", "meta-pill", value)));

  const passing = report.checks.filter((check) => check.status === "passed").length;
  const attention = report.checks.filter((check) => ["failed", "warning"].includes(check.status)).length;
  byId("check-count").textContent = String(report.checks.length);
  byId("pass-count").textContent = String(passing);
  byId("attention-count").textContent = String(attention);
  const generatedDate = new Date(report.generated_at);
  byId("generated-at").textContent = Number.isNaN(generatedDate.getTime())
    ? report.generated_at
    : new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(generatedDate);
  byId("commit").textContent = report.repository.short_sha || "local";

  const grid = byId("check-grid");
  report.checks.forEach((check, index) => {
    const card = make("article", "check-card");
    card.dataset.filter = check.status === "passed" ? "passed" : "attention";
    card.style.setProperty("--delay", `${index * 55}ms`);

    const top = make("div", "check-top");
    const number = make("span", "check-number", String(index + 1).padStart(2, "0"));
    const badge = make("span", `status status-${check.status}`, statusLabel[check.status]);
    top.append(number, badge);

    const title = make("h3", "", check.label);
    const description = make("p", "check-description", check.description);
    const result = make("div", "result-row");
    result.append(
      make("strong", "result-value", check.observed),
      make("span", "weight", `Weight ${check.weight}`),
    );

    const meter = make("div", "meter");
    meter.setAttribute("role", "meter");
    meter.setAttribute("aria-label", `${check.label} score`);
    meter.setAttribute("aria-valuemin", "0");
    meter.setAttribute("aria-valuemax", "100");
    meter.setAttribute("aria-valuenow", String(check.score));
    const fill = make("span", "meter-fill");
    fill.style.width = `${Math.max(0, Math.min(100, check.score))}%`;
    meter.append(fill);

    card.append(top, title, description, result, meter);

    if (check.details.length) {
      const details = make("details", "details");
      const summary = make("summary", "", `${check.details.length} detail${check.details.length === 1 ? "" : "s"}`);
      const list = make("ol", "detail-list");
      check.details.forEach((line) => list.append(make("li", "", line)));
      details.append(summary, list);
      card.append(details);
    }
    grid.append(card);
  });

  document.querySelectorAll(".filter").forEach((button) => {
    button.addEventListener("click", () => {
      const filter = button.dataset.filter;
      document.querySelectorAll(".filter").forEach((item) => {
        const selected = item === button;
        item.classList.toggle("active", selected);
        item.setAttribute("aria-pressed", String(selected));
      });
      document.querySelectorAll(".check-card").forEach((card) => {
        card.hidden = filter !== "all" && card.dataset.filter !== filter;
      });
    });
  });

  const themeButton = byId("theme-toggle");
  let savedTheme = null;
  try { savedTheme = localStorage.getItem("reportcard-theme"); } catch (_) { /* storage is optional */ }
  if (savedTheme) document.documentElement.dataset.theme = savedTheme;
  themeButton.addEventListener("click", () => {
    const current = document.documentElement.dataset.theme;
    const next = current === "dark" ? "light" : current === "light" ? "dark" :
      (matchMedia("(prefers-color-scheme: dark)").matches ? "light" : "dark");
    document.documentElement.dataset.theme = next;
    try { localStorage.setItem("reportcard-theme", next); } catch (_) { /* storage is optional */ }
  });
})();
