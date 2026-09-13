package fs

import (
	"strings"
	"testing"
)

const addOnly = "package testpkg\n\n" +
	"// Add adds two integers.\n" +
	"func Add(a, b int) int {\n" +
	"    return a + b\n" +
	"}\n"

const addAndMultiply = addOnly + "\n" +
	"// Multiply multiplies two integers.\n" +
	"func Multiply(a, b int) int {\n" +
	"    return a * b\n" +
	"}\n"

// The case that started this: a function appended to a file. The model must be
// able to read back that its function is there, and where.
func TestDescribeChange_AnAppendedFunctionIsShownWithItsLineNumbers(t *testing.T) {
	summary, region := describeChange("math.go", addOnly, addAndMultiply)

	if !strings.Contains(summary, "math.go") {
		t.Errorf("the summary must name the file:\n%s", summary)
	}
	if !strings.Contains(summary, "added") {
		t.Errorf("the summary must say what happened:\n%s", summary)
	}
	if !strings.Contains(region, "func Multiply") {
		t.Errorf("the region must show the code that was added, or the model still cannot see its own change:\n%s", region)
	}
	// Multiply's signature is line 9 of the new file. Without real numbers the
	// model cannot tell this region from any other part of the file.
	if !strings.Contains(region, "9: func Multiply(a, b int) int {") {
		t.Errorf("the region must carry the line numbers of the NEW file:\n%s", region)
	}
	// Context, so the change is placed rather than floating.
	if !strings.Contains(region, "func Add") {
		t.Errorf("the region must show a little of what surrounds the change:\n%s", region)
	}
}

// The model must not be told a change happened when none did. A replacement
// that matches what is already there is the exact shape of a restated edit,
// and answering "written" to it is how a turn convinces itself it made
// progress it did not make.
func TestDescribeChange_AnEditThatChangesNothingSaysSo(t *testing.T) {
	summary, region := describeChange("math.go", addAndMultiply, addAndMultiply)

	if !strings.Contains(summary, "unchanged") {
		t.Errorf("an edit that changed nothing must say so:\n%s", summary)
	}
	if region != "" {
		t.Errorf("there is no changed region to show:\n%s", region)
	}
}

func TestDescribeChange_AReplacementReportsBothSides(t *testing.T) {
	before := "a\nb\nc\n"
	after := "a\nB1\nB2\nc\n"

	summary, region := describeChange("x.txt", before, after)

	if !strings.Contains(summary, "line 2") {
		t.Errorf("the summary must say where the change starts:\n%s", summary)
	}
	if !strings.Contains(summary, "replaced") {
		t.Errorf("a replacement must not be reported as a pure addition:\n%s", summary)
	}
	if !strings.Contains(region, "2: B1") || !strings.Contains(region, "3: B2") {
		t.Errorf("the region must show the new lines:\n%s", region)
	}
}

func TestDescribeChange_RemovedLinesAreReported(t *testing.T) {
	before := "a\nb\nc\n"
	after := "a\nc\n"

	summary, _ := describeChange("x.txt", before, after)

	if !strings.Contains(summary, "removed") {
		t.Errorf("a deletion must be reported as one:\n%s", summary)
	}
}

// A new file is a change from nothing, and must not blow the context window.
func TestDescribeChange_ALargeNewFileIsCapped(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 500; i++ {
		b.WriteString("line\n")
	}

	_, region := describeChange("big.txt", "", b.String())

	lines := strings.Count(region, "\n")
	if lines > changedRegionMaxLines+1 {
		t.Errorf("the region is %d lines; a write must not replay the whole file back", lines)
	}
	if !strings.Contains(region, "more changed line") {
		t.Errorf("a truncated region must say it was truncated, or the model reads it as the whole change:\n%s", region)
	}
}

// CRLF must not read as a change on every line.
func TestDescribeChange_LineEndingsAloneAreNotAChange(t *testing.T) {
	summary, _ := describeChange("x.txt", "a\r\nb\r\n", "a\nb\n")

	if !strings.Contains(summary, "unchanged") {
		t.Errorf("only the line endings differ, so no content changed:\n%s", summary)
	}
}
