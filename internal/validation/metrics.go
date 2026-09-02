// Package validation provides metrics collection and export for validation operations
package validation

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"strings"
	"sync"
	"time"
)

// ValidationMetrics provides comprehensive validation metrics with export capabilities
type ValidationMetrics struct {
	mu sync.RWMutex

	// Timing metrics
	TotalDuration        time.Duration `json:"total_duration_ms"`
	ValidationStartTime  time.Time     `json:"validation_start_time"`
	ValidationEndTime    time.Time     `json:"validation_end_time"`
	AverageQueryDuration time.Duration `json:"average_query_duration_ms"`
	SlowestQueryDuration time.Duration `json:"slowest_query_duration_ms"`

	// Count metrics
	TotalValidations      int64 `json:"total_validations"`
	SuccessfulValidations int64 `json:"successful_validations"`
	FailedValidations     int64 `json:"failed_validations"`
	ObjectsValidated      int64 `json:"objects_validated"`
	QueriesExecuted       int64 `json:"queries_executed"`
	ErrorsFound           int64 `json:"errors_found"`
	WarningsFound         int64 `json:"warnings_found"`

	// Schema metrics
	TablesValidated      int64 `json:"tables_validated"`
	IndexesValidated     int64 `json:"indexes_validated"`
	FunctionsValidated   int64 `json:"functions_validated"`
	TriggersValidated    int64 `json:"triggers_validated"`
	ConstraintsValidated int64 `json:"constraints_validated"`
	ViewsValidated       int64 `json:"views_validated"`
	ExtensionsDetected   int64 `json:"extensions_detected"`

	// Docker validation metrics
	DockerContainersSpun int64         `json:"docker_containers_spun"`
	DockerValidationTime time.Duration `json:"docker_validation_time_ms"`
	DockerFailures       int64         `json:"docker_failures"`

	// Error breakdown
	ErrorsByCode     map[string]int64 `json:"errors_by_code"`
	ErrorsBySeverity map[string]int64 `json:"errors_by_severity"`

	// Warning breakdown
	WarningsByCode map[string]int64 `json:"warnings_by_code"`

	// Validation approach metrics
	ApproachUsage map[string]int64 `json:"approach_usage"` // TWO_CONTAINERS, TWO_DATABASES, SCHEMA_DIFF

	// Extension metrics
	ExtensionInstallAttempts int64         `json:"extension_install_attempts"`
	ExtensionInstallFailures int64         `json:"extension_install_failures"`
	ExtensionInstallTime     time.Duration `json:"extension_install_time_ms"`

	// SQL fix metrics
	FixesAttempted int64 `json:"fixes_attempted"`
	FixesSucceeded int64 `json:"fixes_succeeded"`
	FixesFailed    int64 `json:"fixes_failed"`

	// Resource metrics
	PeakMemoryUsage int64         `json:"peak_memory_bytes"`
	CPUTimeUsed     time.Duration `json:"cpu_time_ms"`

	// Metadata
	ValidationLevel   string    `json:"validation_level"`
	PostgreSQLVersion string    `json:"postgresql_version"`
	LastUpdated       time.Time `json:"last_updated"`
}

// NewValidationMetrics creates a new validation metrics collector
func NewValidationMetrics() *ValidationMetrics {
	return &ValidationMetrics{
		ErrorsByCode:     make(map[string]int64),
		ErrorsBySeverity: make(map[string]int64),
		WarningsByCode:   make(map[string]int64),
		ApproachUsage:    make(map[string]int64),
		LastUpdated:      time.Now(),
	}
}

// RecordValidation records a completed validation
func (m *ValidationMetrics) RecordValidation(success bool, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.TotalValidations++
	if success {
		m.SuccessfulValidations++
	} else {
		m.FailedValidations++
	}

	m.TotalDuration += duration
	m.LastUpdated = time.Now()
}

// RecordQuery records a query execution
func (m *ValidationMetrics) RecordQuery(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.QueriesExecuted++

	// Update average
	totalDuration := m.AverageQueryDuration * time.Duration(m.QueriesExecuted-1)
	m.AverageQueryDuration = (totalDuration + duration) / time.Duration(m.QueriesExecuted)

	// Update slowest
	if duration > m.SlowestQueryDuration {
		m.SlowestQueryDuration = duration
	}

	m.LastUpdated = time.Now()
}

// RecordError records an error
func (m *ValidationMetrics) RecordError(code, severity string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ErrorsFound++
	m.ErrorsByCode[code]++
	m.ErrorsBySeverity[severity]++
	m.LastUpdated = time.Now()
}

// RecordWarning records a warning
func (m *ValidationMetrics) RecordWarning(code string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.WarningsFound++
	m.WarningsByCode[code]++
	m.LastUpdated = time.Now()
}

// RecordDockerValidation records Docker validation metrics
func (m *ValidationMetrics) RecordDockerValidation(approach string, duration time.Duration, success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.DockerContainersSpun++
	m.DockerValidationTime += duration
	m.ApproachUsage[approach]++

	if !success {
		m.DockerFailures++
	}

	m.LastUpdated = time.Now()
}

// RecordSchemaObject records a validated schema object
func (m *ValidationMetrics) RecordSchemaObject(objectType string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ObjectsValidated++

	switch strings.ToUpper(objectType) {
	case "TABLE":
		m.TablesValidated++
	case "INDEX":
		m.IndexesValidated++
	case "FUNCTION", "PROCEDURE":
		m.FunctionsValidated++
	case "TRIGGER":
		m.TriggersValidated++
	case "CONSTRAINT":
		m.ConstraintsValidated++
	case "VIEW", "MATERIALIZED VIEW":
		m.ViewsValidated++
	case "EXTENSION":
		m.ExtensionsDetected++
	}

	m.LastUpdated = time.Now()
}

// RecordExtensionInstall records extension installation attempt
func (m *ValidationMetrics) RecordExtensionInstall(success bool, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ExtensionInstallAttempts++
	m.ExtensionInstallTime += duration

	if !success {
		m.ExtensionInstallFailures++
	}

	m.LastUpdated = time.Now()
}

// RecordFix records a SQL fix attempt
func (m *ValidationMetrics) RecordFix(success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.FixesAttempted++

	if success {
		m.FixesSucceeded++
	} else {
		m.FixesFailed++
	}

	m.LastUpdated = time.Now()
}

// UpdateResourceMetrics updates resource usage metrics
func (m *ValidationMetrics) UpdateResourceMetrics(memoryBytes int64, cpuTime time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if memoryBytes > m.PeakMemoryUsage {
		m.PeakMemoryUsage = memoryBytes
	}

	m.CPUTimeUsed = cpuTime
	m.LastUpdated = time.Now()
}

// SetValidationTimes sets validation start and end times
func (m *ValidationMetrics) SetValidationTimes(start, end time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ValidationStartTime = start
	m.ValidationEndTime = end
	m.TotalDuration = end.Sub(start)
	m.LastUpdated = time.Now()
}

// SetValidationLevel sets the validation level metadata
func (m *ValidationMetrics) SetValidationLevel(level string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.ValidationLevel = level
	m.LastUpdated = time.Now()
}

// SetPostgreSQLVersion sets the PostgreSQL version metadata
func (m *ValidationMetrics) SetPostgreSQLVersion(version string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.PostgreSQLVersion = version
	m.LastUpdated = time.Now()
}

// GetSnapshot returns a thread-safe copy of metrics
func (m *ValidationMetrics) GetSnapshot() *ValidationMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snapshot := &ValidationMetrics{
		TotalDuration:            m.TotalDuration,
		ValidationStartTime:      m.ValidationStartTime,
		ValidationEndTime:        m.ValidationEndTime,
		AverageQueryDuration:     m.AverageQueryDuration,
		SlowestQueryDuration:     m.SlowestQueryDuration,
		TotalValidations:         m.TotalValidations,
		SuccessfulValidations:    m.SuccessfulValidations,
		FailedValidations:        m.FailedValidations,
		ObjectsValidated:         m.ObjectsValidated,
		QueriesExecuted:          m.QueriesExecuted,
		ErrorsFound:              m.ErrorsFound,
		WarningsFound:            m.WarningsFound,
		TablesValidated:          m.TablesValidated,
		IndexesValidated:         m.IndexesValidated,
		FunctionsValidated:       m.FunctionsValidated,
		TriggersValidated:        m.TriggersValidated,
		ConstraintsValidated:     m.ConstraintsValidated,
		ViewsValidated:           m.ViewsValidated,
		ExtensionsDetected:       m.ExtensionsDetected,
		DockerContainersSpun:     m.DockerContainersSpun,
		DockerValidationTime:     m.DockerValidationTime,
		DockerFailures:           m.DockerFailures,
		ErrorsByCode:             copyInt64Map(m.ErrorsByCode),
		ErrorsBySeverity:         copyInt64Map(m.ErrorsBySeverity),
		WarningsByCode:           copyInt64Map(m.WarningsByCode),
		ApproachUsage:            copyInt64Map(m.ApproachUsage),
		ExtensionInstallAttempts: m.ExtensionInstallAttempts,
		ExtensionInstallFailures: m.ExtensionInstallFailures,
		ExtensionInstallTime:     m.ExtensionInstallTime,
		FixesAttempted:           m.FixesAttempted,
		FixesSucceeded:           m.FixesSucceeded,
		FixesFailed:              m.FixesFailed,
		PeakMemoryUsage:          m.PeakMemoryUsage,
		CPUTimeUsed:              m.CPUTimeUsed,
		ValidationLevel:          m.ValidationLevel,
		PostgreSQLVersion:        m.PostgreSQLVersion,
		LastUpdated:              m.LastUpdated,
	}

	return snapshot
}

// ExportJSON exports metrics as JSON
func (m *ValidationMetrics) ExportJSON(w io.Writer) error {
	snapshot := m.GetSnapshot()

	// Convert durations to milliseconds for JSON
	jsonMetrics := struct {
		TotalDurationMS          int64            `json:"total_duration_ms"`
		ValidationStartTime      string           `json:"validation_start_time"`
		ValidationEndTime        string           `json:"validation_end_time"`
		AverageQueryDurationMS   int64            `json:"average_query_duration_ms"`
		SlowestQueryDurationMS   int64            `json:"slowest_query_duration_ms"`
		TotalValidations         int64            `json:"total_validations"`
		SuccessfulValidations    int64            `json:"successful_validations"`
		FailedValidations        int64            `json:"failed_validations"`
		SuccessRate              float64          `json:"success_rate_percent"`
		ObjectsValidated         int64            `json:"objects_validated"`
		QueriesExecuted          int64            `json:"queries_executed"`
		ErrorsFound              int64            `json:"errors_found"`
		WarningsFound            int64            `json:"warnings_found"`
		TablesValidated          int64            `json:"tables_validated"`
		IndexesValidated         int64            `json:"indexes_validated"`
		FunctionsValidated       int64            `json:"functions_validated"`
		TriggersValidated        int64            `json:"triggers_validated"`
		ConstraintsValidated     int64            `json:"constraints_validated"`
		ViewsValidated           int64            `json:"views_validated"`
		ExtensionsDetected       int64            `json:"extensions_detected"`
		DockerContainersSpun     int64            `json:"docker_containers_spun"`
		DockerValidationTimeMS   int64            `json:"docker_validation_time_ms"`
		DockerFailures           int64            `json:"docker_failures"`
		ErrorsByCode             map[string]int64 `json:"errors_by_code"`
		ErrorsBySeverity         map[string]int64 `json:"errors_by_severity"`
		WarningsByCode           map[string]int64 `json:"warnings_by_code"`
		ApproachUsage            map[string]int64 `json:"approach_usage"`
		ExtensionInstallAttempts int64            `json:"extension_install_attempts"`
		ExtensionInstallFailures int64            `json:"extension_install_failures"`
		ExtensionInstallTimeMS   int64            `json:"extension_install_time_ms"`
		FixesAttempted           int64            `json:"fixes_attempted"`
		FixesSucceeded           int64            `json:"fixes_succeeded"`
		FixesFailed              int64            `json:"fixes_failed"`
		FixSuccessRate           float64          `json:"fix_success_rate_percent"`
		PeakMemoryBytes          int64            `json:"peak_memory_bytes"`
		CPUTimeMS                int64            `json:"cpu_time_ms"`
		ValidationLevel          string           `json:"validation_level"`
		PostgreSQLVersion        string           `json:"postgresql_version"`
		LastUpdated              string           `json:"last_updated"`
	}{
		TotalDurationMS:          snapshot.TotalDuration.Milliseconds(),
		ValidationStartTime:      snapshot.ValidationStartTime.Format(time.RFC3339),
		ValidationEndTime:        snapshot.ValidationEndTime.Format(time.RFC3339),
		AverageQueryDurationMS:   snapshot.AverageQueryDuration.Milliseconds(),
		SlowestQueryDurationMS:   snapshot.SlowestQueryDuration.Milliseconds(),
		TotalValidations:         snapshot.TotalValidations,
		SuccessfulValidations:    snapshot.SuccessfulValidations,
		FailedValidations:        snapshot.FailedValidations,
		SuccessRate:              calculateSuccessRate(snapshot.SuccessfulValidations, snapshot.TotalValidations),
		ObjectsValidated:         snapshot.ObjectsValidated,
		QueriesExecuted:          snapshot.QueriesExecuted,
		ErrorsFound:              snapshot.ErrorsFound,
		WarningsFound:            snapshot.WarningsFound,
		TablesValidated:          snapshot.TablesValidated,
		IndexesValidated:         snapshot.IndexesValidated,
		FunctionsValidated:       snapshot.FunctionsValidated,
		TriggersValidated:        snapshot.TriggersValidated,
		ConstraintsValidated:     snapshot.ConstraintsValidated,
		ViewsValidated:           snapshot.ViewsValidated,
		ExtensionsDetected:       snapshot.ExtensionsDetected,
		DockerContainersSpun:     snapshot.DockerContainersSpun,
		DockerValidationTimeMS:   snapshot.DockerValidationTime.Milliseconds(),
		DockerFailures:           snapshot.DockerFailures,
		ErrorsByCode:             snapshot.ErrorsByCode,
		ErrorsBySeverity:         snapshot.ErrorsBySeverity,
		WarningsByCode:           snapshot.WarningsByCode,
		ApproachUsage:            snapshot.ApproachUsage,
		ExtensionInstallAttempts: snapshot.ExtensionInstallAttempts,
		ExtensionInstallFailures: snapshot.ExtensionInstallFailures,
		ExtensionInstallTimeMS:   snapshot.ExtensionInstallTime.Milliseconds(),
		FixesAttempted:           snapshot.FixesAttempted,
		FixesSucceeded:           snapshot.FixesSucceeded,
		FixesFailed:              snapshot.FixesFailed,
		FixSuccessRate:           calculateSuccessRate(snapshot.FixesSucceeded, snapshot.FixesAttempted),
		PeakMemoryBytes:          snapshot.PeakMemoryUsage,
		CPUTimeMS:                snapshot.CPUTimeUsed.Milliseconds(),
		ValidationLevel:          snapshot.ValidationLevel,
		PostgreSQLVersion:        snapshot.PostgreSQLVersion,
		LastUpdated:              snapshot.LastUpdated.Format(time.RFC3339),
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(jsonMetrics)
}

// ExportPrometheus exports metrics in Prometheus format
func (m *ValidationMetrics) ExportPrometheus(w io.Writer) error {
	snapshot := m.GetSnapshot()

	var sb strings.Builder

	// Helper to write metric
	writeMetric := func(name, help, mtype string, value any, labels ...string) {
		sb.WriteString(fmt.Sprintf("# HELP %s %s\n", name, help))
		sb.WriteString(fmt.Sprintf("# TYPE %s %s\n", name, mtype))

		labelStr := ""
		if len(labels) > 0 {
			labelStr = "{" + strings.Join(labels, ",") + "}"
		}

		sb.WriteString(fmt.Sprintf("%s%s %v\n\n", name, labelStr, value))
	}

	// Timing metrics
	writeMetric("pgsquash_validation_duration_seconds", "Total validation duration", "gauge",
		snapshot.TotalDuration.Seconds())

	writeMetric("pgsquash_validation_query_duration_avg_seconds", "Average query duration", "gauge",
		snapshot.AverageQueryDuration.Seconds())

	writeMetric("pgsquash_validation_query_duration_max_seconds", "Slowest query duration", "gauge",
		snapshot.SlowestQueryDuration.Seconds())

	// Count metrics
	writeMetric("pgsquash_validations_total", "Total validations performed", "counter",
		snapshot.TotalValidations)

	writeMetric("pgsquash_validations_successful_total", "Successful validations", "counter",
		snapshot.SuccessfulValidations)

	writeMetric("pgsquash_validations_failed_total", "Failed validations", "counter",
		snapshot.FailedValidations)

	writeMetric("pgsquash_validation_objects_total", "Total objects validated", "counter",
		snapshot.ObjectsValidated)

	writeMetric("pgsquash_validation_queries_total", "Total queries executed", "counter",
		snapshot.QueriesExecuted)

	writeMetric("pgsquash_validation_errors_total", "Total errors found", "counter",
		snapshot.ErrorsFound)

	writeMetric("pgsquash_validation_warnings_total", "Total warnings found", "counter",
		snapshot.WarningsFound)

	// Schema object metrics
	writeMetric("pgsquash_validation_tables_total", "Tables validated", "counter",
		snapshot.TablesValidated)

	writeMetric("pgsquash_validation_indexes_total", "Indexes validated", "counter",
		snapshot.IndexesValidated)

	writeMetric("pgsquash_validation_functions_total", "Functions validated", "counter",
		snapshot.FunctionsValidated)

	writeMetric("pgsquash_validation_triggers_total", "Triggers validated", "counter",
		snapshot.TriggersValidated)

	writeMetric("pgsquash_validation_constraints_total", "Constraints validated", "counter",
		snapshot.ConstraintsValidated)

	writeMetric("pgsquash_validation_views_total", "Views validated", "counter",
		snapshot.ViewsValidated)

	writeMetric("pgsquash_validation_extensions_detected", "Extensions detected", "gauge",
		snapshot.ExtensionsDetected)

	// Docker metrics
	writeMetric("pgsquash_docker_containers_total", "Docker containers spun up", "counter",
		snapshot.DockerContainersSpun)

	writeMetric("pgsquash_docker_validation_duration_seconds", "Docker validation time", "gauge",
		snapshot.DockerValidationTime.Seconds())

	writeMetric("pgsquash_docker_failures_total", "Docker validation failures", "counter",
		snapshot.DockerFailures)

	// Error breakdown by code
	for code, count := range snapshot.ErrorsByCode {
		writeMetric("pgsquash_validation_errors_by_code", "Errors by error code", "counter",
			count, fmt.Sprintf(`code="%s"`, code))
	}

	// Error breakdown by severity
	for severity, count := range snapshot.ErrorsBySeverity {
		writeMetric("pgsquash_validation_errors_by_severity", "Errors by severity", "counter",
			count, fmt.Sprintf(`severity="%s"`, severity))
	}

	// Warning breakdown
	for code, count := range snapshot.WarningsByCode {
		writeMetric("pgsquash_validation_warnings_by_code", "Warnings by code", "counter",
			count, fmt.Sprintf(`code="%s"`, code))
	}

	// Approach usage
	for approach, count := range snapshot.ApproachUsage {
		writeMetric("pgsquash_validation_approach_usage", "Validation approach usage", "counter",
			count, fmt.Sprintf(`approach="%s"`, approach))
	}

	// Extension metrics
	writeMetric("pgsquash_extension_install_attempts_total", "Extension install attempts", "counter",
		snapshot.ExtensionInstallAttempts)

	writeMetric("pgsquash_extension_install_failures_total", "Extension install failures", "counter",
		snapshot.ExtensionInstallFailures)

	writeMetric("pgsquash_extension_install_duration_seconds", "Extension install time", "gauge",
		snapshot.ExtensionInstallTime.Seconds())

	// Fix metrics
	writeMetric("pgsquash_fixes_attempted_total", "SQL fixes attempted", "counter",
		snapshot.FixesAttempted)

	writeMetric("pgsquash_fixes_succeeded_total", "SQL fixes succeeded", "counter",
		snapshot.FixesSucceeded)

	writeMetric("pgsquash_fixes_failed_total", "SQL fixes failed", "counter",
		snapshot.FixesFailed)

	// Resource metrics
	writeMetric("pgsquash_memory_peak_bytes", "Peak memory usage", "gauge",
		snapshot.PeakMemoryUsage)

	writeMetric("pgsquash_cpu_time_seconds", "CPU time used", "gauge",
		snapshot.CPUTimeUsed.Seconds())

	// Success rates
	successRate := calculateSuccessRate(snapshot.SuccessfulValidations, snapshot.TotalValidations)
	writeMetric("pgsquash_validation_success_rate", "Validation success rate (0-1)", "gauge",
		successRate/100.0)

	fixSuccessRate := calculateSuccessRate(snapshot.FixesSucceeded, snapshot.FixesAttempted)
	writeMetric("pgsquash_fix_success_rate", "Fix success rate (0-1)", "gauge",
		fixSuccessRate/100.0)

	_, err := w.Write([]byte(sb.String()))
	return err
}

// Reset resets all metrics to zero
func (m *ValidationMetrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	*m = ValidationMetrics{
		ErrorsByCode:     make(map[string]int64),
		ErrorsBySeverity: make(map[string]int64),
		WarningsByCode:   make(map[string]int64),
		ApproachUsage:    make(map[string]int64),
		LastUpdated:      time.Now(),
	}
}

// Helper functions

func copyInt64Map(src map[string]int64) map[string]int64 {
	dst := make(map[string]int64, len(src))
	maps.Copy(dst, src)
	return dst
}

func calculateSuccessRate(successful, total int64) float64 {
	if total == 0 {
		return 0.0
	}
	return (float64(successful) / float64(total)) * 100.0
}
