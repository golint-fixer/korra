package korra

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// Reporter is an interface defining Report computation.
type Reporter interface {
	Report(Results) ([]byte, error)
}

// ReporterFunc is an adapter to allow the use of ordinary functions as
// Reporters. If f is a function with the appropriate signature, ReporterFunc(f)
// is a Reporter object that calls f.
type ReporterFunc func(Results) ([]byte, error)

// Report implements the Reporter interface.
func (f ReporterFunc) Report(r Results) ([]byte, error) { return f(r) }

// HistogramReporter is a reporter that computes latency histograms with the
// given buckets.
type HistogramReporter []time.Duration

// Report implements the Reporter interface.
func (h HistogramReporter) Report(r Results) ([]byte, error) {
	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 8, 2, ' ', tabwriter.StripEscape)

	bucket := func(i int) string {
		if i+1 >= len(h) {
			return fmt.Sprintf("[%s,\t+Inf]", h[i])
		}
		return fmt.Sprintf("[%s,\t%s]", h[i], h[i+1])
	}

	fmt.Fprintf(w, "Bucket\t\t#\t%%\tHistogram\n")
	for i, count := range Histogram(h, r) {
		ratio := float64(count) / float64(len(r))
		fmt.Fprintf(w, "%s\t%d\t%.2f%%\t%s\n",
			bucket(i),
			count,
			ratio*100,
			strings.Repeat("#", int(ratio*75)),
		)
	}

	err := w.Flush()
	return buf.Bytes(), err
}

// Set implements the flag.Value interface.
func (h *HistogramReporter) Set(value string) error {
	for _, v := range strings.Split(value[1:len(value)-1], ",") {
		d, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		*h = append(*h, d)
	}
	if len(*h) == 0 {
		return fmt.Errorf("bad buckets: %s", value)
	}
	return nil
}

// String implements the fmt.Stringer interface.
func (h HistogramReporter) String() string {
	strs := make([]string, len(h))
	for i := range strs {
		strs[i] = strconv.FormatInt(int64(h[i]), 10)
	}
	return "[" + strings.Join(strs, ",") + "]"
}

// TextReporter returns a set of computed Metrics structs as aligned, formatted
// text -- one for overall performance, and one for each URL bucket.
type TextReporter struct {
	Collection BucketCollection
	ShowUrls   bool
}

func (tr TextReporter) Report(r Results) ([]byte, error) {
	var err error

	// first display overall results
	out := &bytes.Buffer{}
	fmt.Fprintf(out, "OVERALL: %d results\n", len(r))
	if err = resultsToText(out, tr.ShowUrls, r, make(map[string]uint32)); err != nil {
		return []byte{}, err
	}

	// then display results per URL bucket
	// ...if no buckets infer from results
	tr.Collection.AddResults(r)

	// ...then display results for each
	for _, bucket := range tr.Collection.Buckets() {
		fmt.Fprintf(out, "%s: %d results\n", bucket.String(), len(bucket.Results))
		if err = resultsToText(out, tr.ShowUrls, bucket.Results, bucket.Urls); err != nil {
			return []byte{}, err
		}
	}
	catchAll := tr.Collection.CatchAllBucket()
	if catchAll != nil && len(catchAll.Results) > 0 {
		fmt.Fprintf(out, "Remaining: %d results\n", len(catchAll.Results))
		resultsToText(out, tr.ShowUrls, catchAll.Results, catchAll.Urls)
	}
	return out.Bytes(), nil
}

func resultsToText(out io.Writer, showUrls bool, r Results, urlCounts map[string]uint32) error {
	m := NewMetrics(r)
	w := tabwriter.NewWriter(out, 0, 8, 2, '\t', tabwriter.StripEscape)
	fmt.Fprintf(w, "Requests\t[total]\t%d\n", m.Requests)
	fmt.Fprintf(w, "Duration\t[total, attack, wait]\t%s, %s, %s\n", m.Duration+m.Wait, m.Duration, m.Wait)
	fmt.Fprintf(w, "Latencies\t[mean, 50, 95, 99, max]\t%s, %s, %s, %s, %s\n",
		m.Latencies.Mean, m.Latencies.P50, m.Latencies.P95, m.Latencies.P99, m.Latencies.Max)
	fmt.Fprintf(w, "Bytes In\t[total, mean]\t%d, %.2f\n", m.BytesIn.Total, m.BytesIn.Mean)
	fmt.Fprintf(w, "Bytes Out\t[total, mean]\t%d, %.2f\n", m.BytesOut.Total, m.BytesOut.Mean)
	fmt.Fprintf(w, "Success\t[ratio]\t%.2f%%\n", m.Success*100)
	fmt.Fprintf(w, "Status Codes\t[code:count]\t")
	for code, count := range m.StatusCodes {
		fmt.Fprintf(w, "%s:%d  ", code, count)
	}
	errorCount := strconv.Itoa(len(m.Errors))
	if errorCount == "0" {
		errorCount = "(empty)"
	}
	fmt.Fprintf(w, "\nError Set: %s\n", errorCount)
	for _, err := range m.Errors {
		fmt.Fprintln(w, err)
	}
	if showUrls {
		fmt.Fprintf(w, "URLs in bucket:\n")
		sorted := make([]string, len(urlCounts))
		idx := 0
		for url, _ := range urlCounts {
			sorted[idx] = url
			idx++
		}
		sort.Strings(sorted)
		for _, url := range sorted {
			fmt.Fprintf(w, "\t%s: %d\n", url, urlCounts[url])
		}
	}
	return w.Flush()
}

// ReportJSON writes a computed Metrics struct to as JSON
var ReportJSON ReporterFunc = func(r Results) ([]byte, error) {
	return json.Marshal(NewMetrics(r))
}
