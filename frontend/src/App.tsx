import { useEffect, useMemo, useState } from 'react'
import { Dashboard, eventsURL, getDashboard, setChaos, startLoad, startRush } from './api'

const stages = ['placed', 'confirmed', 'preparing', 'ready', 'out_for_delivery', 'delivered', 'failed', 'cancelled']

export default function App() {
  const [dashboard, setDashboard] = useState<Dashboard | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)

  async function refresh() {
    try {
      setDashboard(await getDashboard())
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  useEffect(() => {
    refresh()
    const interval = setInterval(refresh, 3000)
    const events = new EventSource(eventsURL())
    events.addEventListener('dashboard', (event) => {
      setDashboard(JSON.parse((event as MessageEvent).data))
    })
    events.onerror = () => setError('SSE disconnected; polling still active')
    return () => {
      clearInterval(interval)
      events.close()
    }
  }, [])

  const totals = dashboard?.totals
  const events = useMemo(() => dashboard?.recent_events ?? [], [dashboard])

  async function action(label: string, fn: () => Promise<unknown>) {
    try {
      setNotice(`${label} triggered`)
      await fn()
      await refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100">
      <section className="mx-auto max-w-7xl px-6 py-8">
        <div className="flex flex-col gap-4 md:flex-row md:items-end md:justify-between">
          <div>
            <p className="text-sm uppercase tracking-[0.3em] text-cyan-300">Food Delivery Ops</p>
            <h1 className="mt-2 text-4xl font-bold">Order Pipeline Dashboard</h1>
            <p className="mt-2 text-slate-300">Live view of order flow, backlog, retries, failures, and recovery.</p>
          </div>
          <div className="rounded-xl border border-slate-700 bg-slate-900 px-4 py-3 text-sm">
            {error ? <span className="text-rose-300">{error}</span> : <span className="text-emerald-300">Connected</span>}
            {notice && <div className="mt-1 text-slate-400">{notice}</div>}
          </div>
        </div>

        <div className="mt-8 grid gap-4 md:grid-cols-4">
          <Metric title="Total Orders" value={totals?.orders_total ?? 0} />
          <Metric title="In Flight" value={totals?.in_flight ?? 0} />
          <Metric title="Delivered" value={totals?.delivered ?? 0} />
          <Metric title="Failed" value={totals?.failed ?? 0} danger={(totals?.failed ?? 0) > 0} />
          <Metric title="Pending Outbox" value={totals?.pending_outbox ?? 0} warning={(totals?.pending_outbox ?? 0) > 20} />
          <Metric title="Oldest Pending Sec" value={totals?.oldest_pending_seconds ?? 0} warning={(totals?.oldest_pending_seconds ?? 0) > 10} />
          <Metric title="Max Retry Count" value={totals?.max_attempt_count ?? 0} warning={(totals?.max_attempt_count ?? 0) > 0} />
          <Metric title="Events / 60s" value={totals?.recent_events_window ?? 0} />
        </div>

        <div className="mt-8 grid gap-6 lg:grid-cols-[1.4fr_1fr]">
          <section className="rounded-2xl border border-slate-800 bg-slate-900/80 p-5">
            <h2 className="text-xl font-semibold">Pipeline Stages</h2>
            <div className="mt-5 grid gap-3 md:grid-cols-4">
              {stages.map((stage) => (
                <div key={stage} className="rounded-xl border border-slate-700 bg-slate-950 p-4">
                  <div className="text-sm uppercase tracking-wider text-slate-400">{stage.replaceAll('_', ' ')}</div>
                  <div className="mt-3 text-3xl font-bold">{dashboard?.status_counts?.[stage] ?? 0}</div>
                </div>
              ))}
            </div>
          </section>

          <section className="rounded-2xl border border-slate-800 bg-slate-900/80 p-5">
            <h2 className="text-xl font-semibold">Demo Controls</h2>
            <div className="mt-5 grid gap-3">
              <button className="btn" onClick={() => action('Small load', () => startLoad(5, 30))}>Start Small Load</button>
              <button className="btn btn-hot" onClick={() => action('Dinner rush', () => startRush())}>Cause Dinner Rush</button>
              <button className="btn btn-warn" onClick={() => action('Restaurant degraded', () => setChaos('restaurant', 0.75))}>Degrade Restaurant</button>
              <button className="btn" onClick={() => action('Restaurant recovered', () => setChaos('restaurant', 0))}>Recover Restaurant</button>
              <button className="btn btn-warn" onClick={() => action('Courier degraded', () => setChaos('courier', 0.65))}>Degrade Courier</button>
              <button className="btn" onClick={() => action('Courier recovered', () => setChaos('courier', 0))}>Recover Courier</button>
            </div>
          </section>
        </div>

        <section className="mt-8 rounded-2xl border border-slate-800 bg-slate-900/80 p-5">
          <h2 className="text-xl font-semibold">Recent Order Events</h2>
          <div className="mt-4 overflow-hidden rounded-xl border border-slate-800">
            <table className="w-full text-left text-sm">
              <thead className="bg-slate-950 text-slate-400">
                <tr>
                  <th className="px-4 py-3">Time</th>
                  <th className="px-4 py-3">Order</th>
                  <th className="px-4 py-3">Transition</th>
                  <th className="px-4 py-3">Reason</th>
                </tr>
              </thead>
              <tbody>
                {events.map((e) => (
                  <tr key={e.id} className="border-t border-slate-800">
                    <td className="px-4 py-3 text-slate-400">{new Date(e.created_at).toLocaleTimeString()}</td>
                    <td className="px-4 py-3 font-mono text-xs">{e.order_id.slice(0, 8)}</td>
                    <td className="px-4 py-3">{e.from_status ?? 'new'} → <span className="text-cyan-300">{e.to_status}</span></td>
                    <td className="px-4 py-3 text-slate-400">{e.reason ?? ''}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      </section>
    </main>
  )
}

function Metric({ title, value, danger, warning }: { title: string; value: number; danger?: boolean; warning?: boolean }) {
  const color = danger ? 'text-rose-300' : warning ? 'text-amber-300' : 'text-cyan-300'
  return (
    <div className="rounded-2xl border border-slate-800 bg-slate-900 p-5">
      <div className="text-sm uppercase tracking-wider text-slate-400">{title}</div>
      <div className={`mt-3 text-3xl font-bold ${color}`}>{value}</div>
    </div>
  )
}
