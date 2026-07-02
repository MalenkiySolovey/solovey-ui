package componentdoctor

import (
	"flag"
	"fmt"
	"io"
	"strings"

	componentdoctor "github.com/MalenkiySolovey/solovey-ui/componenthost/doctor"
	configstorage "github.com/MalenkiySolovey/solovey-ui/config/storage"
	dbsqlite "github.com/MalenkiySolovey/solovey-ui/database/sqlite"
)

func Run(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(out)

	var components bool
	fs.BoolVar(&components, "components", false, "show installed component metadata, binary presence, enabled state and orphan data")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if !components {
		fs.Usage()
		return 2
	}

	if err := dbsqlite.Init(configstorage.GetDBPath()); err != nil {
		fmt.Fprintln(out, "doctor:", err)
		return 1
	}
	defer func() {
		if err := dbsqlite.Close(); err != nil {
			fmt.Fprintln(out, "doctor: close database:", err)
		}
	}()

	report := componentdoctor.Inspect(dbsqlite.DB())
	Print(out, report)
	if componentdoctor.HasErrors(report) {
		return 1
	}
	return 0
}

func Print(out io.Writer, report componentdoctor.Report) {
	fmt.Fprintln(out, "Solovey UI components")
	fmt.Fprintf(out, "Installed metadata: %s\n", report.InstalledPath)
	if report.MetadataPresent {
		fmt.Fprintln(out, "Metadata: present")
		if strings.TrimSpace(report.MetadataBinary) != "" {
			fmt.Fprintf(out, "Binary profile: %s\n", report.MetadataBinary)
		}
	} else {
		fmt.Fprintln(out, "Metadata: missing, no optional components are installed implicitly")
	}
	if report.MetadataError != "" {
		fmt.Fprintf(out, "Metadata error: %s\n", report.MetadataError)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%-34s %-9s %-9s %-7s %-7s %-10s %-12s %s\n",
		"ID",
		"Available",
		"Installed",
		"Enabled",
		"Active",
		"Delivery",
		"Version",
		"Issues",
	)
	for _, row := range report.Rows {
		fmt.Fprintf(out, "%-34s %-9s %-9s %-7s %-7s %-10s %-12s %s\n",
			row.ID,
			yesNo(row.Available),
			yesNo(row.Installed),
			yesNo(row.Enabled),
			yesNo(row.Active),
			valueOrDash(row.Delivery),
			valueOrDash(row.Version),
			issueSummary(row.Issues),
		)
	}
	if len(report.Rows) == 0 {
		fmt.Fprintln(out, "No installed component metadata or orphan component data found.")
	}

	if len(report.Issues) == 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Component state is consistent.")
		return
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Issues:")
	for _, row := range report.Rows {
		for _, issue := range row.Issues {
			fmt.Fprintf(out, "- [%s] %s: %s\n", issue.Severity, row.ID, issue.Message)
		}
	}
	for _, issue := range report.Issues {
		if issue.Message == "" {
			continue
		}
		if reportHasRowIssue(report.Rows, issue) {
			continue
		}
		fmt.Fprintf(out, "- [%s] %s\n", issue.Severity, issue.Message)
	}
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func valueOrDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}

func issueSummary(issues []componentdoctor.Issue) string {
	if len(issues) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, issue.Severity)
	}
	return strings.Join(parts, ",")
}

func reportHasRowIssue(rows []componentdoctor.Row, target componentdoctor.Issue) bool {
	for _, row := range rows {
		for _, issue := range row.Issues {
			if issue == target {
				return true
			}
		}
	}
	return false
}
