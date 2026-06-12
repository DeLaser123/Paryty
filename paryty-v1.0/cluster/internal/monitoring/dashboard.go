package monitoring

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sync"
	"time"
)

// Dashboard serves a simple HTML health dashboard
type Dashboard struct {
	monitor     *SelfMonitor
	alertMgr    *AlertManager
	healthCheck *HealthFallback
	mu          sync.RWMutex
	startTime   time.Time
}

// DashboardConfig configures the Dashboard
type DashboardConfig struct {
	Monitor     *SelfMonitor
	AlertMgr    *AlertManager
	HealthCheck *HealthFallback
}

// NewDashboard creates a new Dashboard
func NewDashboard(cfg DashboardConfig) *Dashboard {
	return &Dashboard{
		monitor:     cfg.Monitor,
		alertMgr:    cfg.AlertMgr,
		healthCheck: cfg.HealthCheck,
		startTime:   time.Now(),
	}
}

// RegisterRoutes registers dashboard HTTP routes
func (d *Dashboard) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/health", d.handleDashboard)
	mux.HandleFunc("/admin/health/api", d.handleHealthAPI)
	mux.HandleFunc("/admin/alerts", d.handleAlerts)
	mux.HandleFunc("/metrics", d.handleMetrics)
}

// handleDashboard serves the HTML dashboard
func (d *Dashboard) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	data := struct {
		Title     string
		Uptime    string
		Timestamp string
	}{
		Title:     "Paryty Health Dashboard",
		Uptime:    time.Since(d.startTime).Round(time.Second).String(),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	tmpl.Execute(w, data)
}

// handleHealthAPI returns health data as JSON
func (d *Dashboard) handleHealthAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	report := d.healthCheck.CheckAll(r.Context(), StandardServices())

	json.NewEncoder(w).Encode(report)
}

// handleAlerts returns active alerts as JSON
func (d *Dashboard) handleAlerts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	alerts := d.alertMgr.GetActiveAlerts()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"alerts": alerts,
		"count":  len(alerts),
	})
}

// handleMetrics returns current metrics in Prometheus format
func (d *Dashboard) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	// Get health report
	report := d.healthCheck.CheckAll(r.Context(), StandardServices())

	// Output Prometheus-style metrics
	fmt.Fprintf(w, "# HELP paryty_uptime_seconds Time since Paryty started\n")
	fmt.Fprintf(w, "# TYPE paryty_uptime_seconds gauge\n")
	fmt.Fprintf(w, "paryty_uptime_seconds %f\n", time.Since(d.startTime).Seconds())

	fmt.Fprintf(w, "# HELP paryty_service_healthy Whether a service is healthy (1) or not (0)\n")
	fmt.Fprintf(w, "# TYPE paryty_service_healthy gauge\n")
	for _, svc := range report.Services {
		healthy := 0
		if svc.Healthy {
			healthy = 1
		}
		fmt.Fprintf(w, "paryty_service_healthy{service=\"%s\",address=\"%s\"} %d\n",
			svc.Name, svc.Address, healthy)
	}

	fmt.Fprintf(w, "# HELP paryty_service_latency_seconds Service check latency\n")
	fmt.Fprintf(w, "# TYPE paryty_service_latency_seconds gauge\n")
	for _, svc := range report.Services {
		fmt.Fprintf(w, "paryty_service_latency_seconds{service=\"%s\",address=\"%s\"} %f\n",
			svc.Name, svc.Address, svc.Latency.Seconds())
	}

	// Alert metrics
	alerts := d.alertMgr.GetActiveAlerts()
	fmt.Fprintf(w, "# HELP paryty_alerts_active Number of active alerts\n")
	fmt.Fprintf(w, "# TYPE paryty_alerts_active gauge\n")
	fmt.Fprintf(w, "paryty_alerts_active %d\n", len(alerts))

	fmt.Fprintf(w, "# HELP paryty_alert_firing Whether an alert is firing (1) or not (0)\n")
	fmt.Fprintf(w, "# TYPE paryty_alert_firing gauge\n")
	for _, alert := range alerts {
		firing := 0
		if alert.Status == AlertStatusFiring {
			firing = 1
		}
		fmt.Fprintf(w, "paryty_alert_firing{alert=\"%s\",severity=\"%s\"} %d\n",
			alert.Name, alert.Severity, firing)
	}
}

// Dashboard HTML template
var tmpl = template.Must(template.New("dashboard").Parse(`<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{ .Title }}</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
            max-width: 1200px;
            margin: 0 auto;
            padding: 20px;
            background: #f5f5f5;
        }
        .header {
            background: #1a1a2e;
            color: white;
            padding: 20px;
            border-radius: 8px;
            margin-bottom: 20px;
        }
        .card {
            background: white;
            padding: 20px;
            border-radius: 8px;
            margin-bottom: 20px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        .healthy { color: #22c55e; }
        .unhealthy { color: #ef4444; }
        .warning { color: #f59e0b; }
        table {
            width: 100%;
            border-collapse: collapse;
        }
        th, td {
            padding: 12px;
            text-align: left;
            border-bottom: 1px solid #e5e7eb;
        }
        th {
            background: #f9fafb;
            font-weight: 600;
        }
        .status-badge {
            padding: 4px 8px;
            border-radius: 4px;
            font-size: 14px;
            font-weight: 500;
        }
        .status-healthy { background: #dcfce7; color: #166534; }
        .status-unhealthy { background: #fee2e2; color: #991b1b; }
        .status-pending { background: #fef3c7; color: #92400e; }
        .status-firing { background: #fef2f2; color: #991b1b; }
    </style>
</head>
<body>
    <div class="header">
        <h1>{{ .Title }}</h1>
        <p>Uptime: {{ .Uptime }} | Last Updated: {{ .Timestamp }}</p>
    </div>

    <div class="card" id="services">
        <h2>Service Health</h2>
        <table>
            <thead>
                <tr>
                    <th>Service</th>
                    <th>Address</th>
                    <th>Status</th>
                    <th>Latency</th>
                    <th>Last Check</th>
                </tr>
            </thead>
            <tbody id="service-list">
                <tr><td colspan="5">Loading...</td></tr>
            </tbody>
        </table>
    </div>

    <div class="card" id="alerts">
        <h2>Active Alerts</h2>
        <table>
            <thead>
                <tr>
                    <th>Alert</th>
                    <th>Severity</th>
                    <th>Status</th>
                    <th>Message</th>
                    <th>Since</th>
                </tr>
            </thead>
            <tbody id="alert-list">
                <tr><td colspan="5">Loading...</td></tr>
            </tbody>
        </table>
    </div>

    <script>
        async function loadData() {
            // Load services
            try {
                const healthResp = await fetch('/admin/health/api');
                const healthData = await healthResp.json();
                const serviceList = document.getElementById('service-list');
                serviceList.innerHTML = healthData.services.map(svc => '` + "`" + `
                    <tr>
                        <td>${svc.name}</td>
                        <td>${svc.address}</td>
                        <td><span class="status-badge status-${svc.healthy ? 'healthy' : 'unhealthy'}">${svc.healthy ? 'Healthy' : 'Unhealthy'}</span></td>
                        <td>${(svc.latency / 1000000).toFixed(2)}ms</td>
                        <td>${new Date(svc.checked_at).toLocaleString()}</td>
                    </tr>
                ` + "`" + `).join('');
            } catch (e) {
                console.error('Failed to load health data:', e);
            }

            // Load alerts
            try {
                const alertResp = await fetch('/admin/alerts');
                const alertData = await alertResp.json();
                const alertList = document.getElementById('alert-list');
                if (alertData.alerts.length === 0) {
                    alertList.innerHTML = '<tr><td colspan="5">No active alerts</td></tr>';
                } else {
                    alertList.innerHTML = alertData.alerts.map(alert => '` + "`" + `
                        <tr>
                            <td>${alert.name}</td>
                            <td><span class="status-badge status-${alert.severity}">${alert.severity}</span></td>
                            <td><span class="status-badge status-${alert.status}">${alert.status}</span></td>
                            <td>${alert.message}</td>
                            <td>${new Date(alert.started_at).toLocaleString()}</td>
                        </tr>
                    ` + "`" + `).join('');
                }
            } catch (e) {
                console.error('Failed to load alerts:', e);
            }
        }

        loadData();
        setInterval(loadData, 30000); // Refresh every 30 seconds
    </script>
</body>
</html>
`))
