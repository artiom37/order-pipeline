const API_BASE = import.meta.env.VITE_API_BASE || 'http://localhost:8080'

export type OrderEvent = {
  id: number
  order_id: string
  from_status?: string
  to_status: string
  reason?: string
  created_at: string
}

export type Dashboard = {
  status_counts: Record<string, number>
  recent_events: OrderEvent[]
  totals: {
    orders_total: number
    in_flight: number
    delivered: number
    failed: number
    pending_outbox: number
    max_attempt_count: number
    oldest_pending_seconds: number
    recent_events_window: number
  }
}

export async function getDashboard(): Promise<Dashboard> {
  const res = await fetch(`${API_BASE}/api/dashboard`)
  if (!res.ok) throw new Error(`dashboard failed: ${res.status}`)
  return res.json()
}

export function eventsURL(): string {
  return `${API_BASE}/api/events`
}

export async function startLoad(ordersPerSecond: number, durationSeconds: number) {
  const res = await fetch(`${API_BASE}/api/load/start`, {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({orders_per_second: ordersPerSecond, duration_seconds: durationSeconds})
  })
  if (!res.ok) throw new Error(await res.text())
  return res.json()
}

export async function startRush() {
  const res = await fetch(`${API_BASE}/api/load/rush`, { method: 'POST' })
  if (!res.ok) throw new Error(await res.text())
  return res.json()
}

export async function setChaos(target: 'restaurant' | 'courier', failureRate: number) {
  const res = await fetch(`${API_BASE}/api/chaos/${target}`, {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify({failure_rate: failureRate, min_delay_ms: 300, max_delay_ms: failureRate > 0 ? 2500 : 800})
  })
  if (!res.ok) throw new Error(await res.text())
  return res.json()
}
