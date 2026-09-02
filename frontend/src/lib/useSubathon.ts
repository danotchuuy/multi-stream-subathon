import { useEffect, useRef, useState } from 'react'
import type { Snapshot } from '../types'

const RECONNECT_DELAY_MS = 2000

function wsUrl(timerId: string): string {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${protocol}//${window.location.host}/ws/${timerId}`
}

interface SubathonConnection {
  snapshot: Snapshot | null
  connected: boolean
}

/**
 * Subscribes to the live snapshot for one timer over WebSocket,
 * reconnecting automatically if the connection drops. Pass the timer ID
 * from the URL (the "token" identifying which timer to watch).
 */
export function useSubathon(timerId: string | undefined): SubathonConnection {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null)
  const [connected, setConnected] = useState(false)
  const reconnectTimer = useRef<ReturnType<typeof setTimeout>>(undefined)

  useEffect(() => {
    if (!timerId) return

    let socket: WebSocket
    let cancelled = false

    const connect = () => {
      socket = new WebSocket(wsUrl(timerId))

      socket.onopen = () => setConnected(true)

      socket.onmessage = (event) => {
        try {
          setSnapshot(JSON.parse(event.data) as Snapshot)
        } catch {
          // ignore malformed frames
        }
      }

      socket.onclose = () => {
        setConnected(false)
        if (!cancelled) {
          reconnectTimer.current = setTimeout(connect, RECONNECT_DELAY_MS)
        }
      }

      socket.onerror = () => socket.close()
    }

    connect()

    return () => {
      cancelled = true
      clearTimeout(reconnectTimer.current)
      socket?.close()
    }
  }, [timerId])

  return { snapshot, connected }
}
