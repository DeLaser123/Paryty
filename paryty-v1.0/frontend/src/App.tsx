import { BrowserRouter as Router, Routes, Route } from 'react-router-dom'
import TopologyView from './components/TopologyView'
import MetricsView from './components/MetricsView'
import TimelineView from './components/TimelineView'
import AlertView from './components/AlertView'

function App() {
  return (
    <Router>
      <div className="app">
        <header className="app-header">
          <h1>Paryty</h1>
          <nav>
            <a href="/">Topology</a>
            <a href="/metrics">Metrics</a>
            <a href="/timeline">Timeline</a>
            <a href="/alerts">Alerts</a>
          </nav>
        </header>
        <main className="app-main">
          <Routes>
            <Route path="/" element={<TopologyView />} />
            <Route path="/metrics" element={<MetricsView />} />
            <Route path="/timeline" element={<TimelineView />} />
            <Route path="/alerts" element={<AlertView />} />
          </Routes>
        </main>
      </div>
    </Router>
  )
}

export default App
