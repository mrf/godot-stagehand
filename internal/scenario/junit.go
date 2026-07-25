package scenario

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// JUnit XML, as consumed by GitHub Actions test reporters, GitLab, Jenkins and
// the rest. One testsuite per scenario, one testcase per step.
type junitSuites struct {
	XMLName  xml.Name     `xml:"testsuites"`
	Name     string       `xml:"name,attr"`
	Tests    int          `xml:"tests,attr"`
	Failures int          `xml:"failures,attr"`
	Errors   int          `xml:"errors,attr"`
	Skipped  int          `xml:"skipped,attr"`
	Time     string       `xml:"time,attr"`
	Suites   []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name       string      `xml:"name,attr"`
	Tests      int         `xml:"tests,attr"`
	Failures   int         `xml:"failures,attr"`
	Errors     int         `xml:"errors,attr"`
	Skipped    int         `xml:"skipped,attr"`
	Time       string      `xml:"time,attr"`
	Timestamp  string      `xml:"timestamp,attr,omitempty"`
	Properties []junitProp `xml:"properties>property,omitempty"`
	Cases      []junitCase `xml:"testcase"`
	SystemOut  string      `xml:"system-out,omitempty"`
}

type junitProp struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Error     *junitFailure `xml:"error,omitempty"`
	Skipped   *junitSkipped `xml:"skipped,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Body    string `xml:",chardata"`
}

type junitSkipped struct {
	Message string `xml:"message,attr"`
}

// WriteJUnit writes the run as a JUnit XML report.
//
// The distinction the format cares about is honoured deliberately: an
// assertion that did not hold is a <failure> (the game behaved wrongly), while
// a connection, usage or timeout problem is an <error> (the test never got to
// judge the game). CI dashboards colour and triage those differently.
func (r *Report) WriteJUnit(path string) error {
	suite := junitSuite{
		Name:      r.Name,
		Time:      seconds(r.DurationMs),
		Timestamp: r.StartedAt,
		Properties: []junitProp{
			{Name: "stagehand.version", Value: r.StagehandVersion},
			{Name: "stagehand.protocol", Value: r.Protocol},
			{Name: "godot.version", Value: r.EngineVersion},
			{Name: "target.mode", Value: r.Target.Mode},
			{Name: "rpc.count", Value: fmt.Sprint(r.RPC.Count)},
		},
	}

	for _, step := range r.Steps {
		suite.Cases = append(suite.Cases, junitCaseFor(r.Name, "step", step))
	}
	for _, step := range r.Teardown {
		suite.Cases = append(suite.Cases, junitCaseFor(r.Name, "teardown", step))
	}

	for _, c := range suite.Cases {
		suite.Tests++
		switch {
		case c.Failure != nil:
			suite.Failures++
		case c.Error != nil:
			suite.Errors++
		case c.Skipped != nil:
			suite.Skipped++
		}
	}
	suite.SystemOut = r.Summary()

	doc := junitSuites{
		Name:     r.Name,
		Tests:    suite.Tests,
		Failures: suite.Failures,
		Errors:   suite.Errors,
		Skipped:  suite.Skipped,
		Time:     seconds(r.DurationMs),
		Suites:   []junitSuite{suite},
	}

	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode junit report: %w", err)
	}
	return writeArtifact(path, append([]byte(xml.Header), append(body, '\n')...))
}

func junitCaseFor(scenarioName, phase string, step StepResult) junitCase {
	testCase := junitCase{
		Name:      fmt.Sprintf("%02d %s", step.Index, step.Name),
		ClassName: fmt.Sprintf("%s.%s.%s", scenarioName, phase, step.Action),
		Time:      seconds(step.DurationMs),
	}
	switch step.Status {
	case StatusSkipped:
		testCase.Skipped = &junitSkipped{Message: "not reached: an earlier step failed"}
	case StatusFailed:
		detail := &junitFailure{
			Message: firstLine(step.Error),
			Type:    step.ErrorKind,
			Body:    junitBody(step),
		}
		if step.ErrorKind == KindAssertion {
			testCase.Failure = detail
		} else {
			testCase.Error = detail
		}
	}
	return testCase
}

func junitBody(step StepResult) string {
	var body strings.Builder
	body.WriteString(step.Error)
	if len(step.Result) > 0 {
		body.WriteString("\n\nresult: ")
		body.Write(step.Result)
	}
	if len(step.Artifacts) > 0 {
		body.WriteString("\n\nartifacts:\n  " + strings.Join(step.Artifacts, "\n  "))
	}
	return body.String()
}

func seconds(ms int64) string {
	return fmt.Sprintf("%.3f", float64(ms)/1000)
}

func firstLine(s string) string {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx]
	}
	return s
}
