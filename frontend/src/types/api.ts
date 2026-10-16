export interface User {
  id: string
  email: string
  full_name: string
  role: 'CUSTOMER' | 'ADMIN'
  created_at: string
}

export interface Event {
  id: string
  title: string
  description: string
  category: string
  venue: string
  banner_url?: string
  sessions?: Session[]
}

export interface Session {
  id: string
  event_id: string
  title: string
  start_time: string
  end_time: string
  status: string
  total_seats: number
  available_seats: number
}

export type SeatStatus = 'AVAILABLE' | 'HELD' | 'BOOKED' | 'UNAVAILABLE'

export interface Seat {
  id: string
  session_id: string
  row_label: string
  seat_number: number
  seat_category: 'VIP' | 'PREMIUM' | 'STANDARD' | 'BALCONY'
  base_price_cents: number
  status: SeatStatus
  version: number
  rowLabel?: string
  seatNumber?: number
  seatCategory?: 'VIP' | 'PREMIUM' | 'STANDARD' | 'BALCONY'
  basePriceCents?: number
}

export interface Reservation {
  id: string
  session_id: string
  user_id: string
  status: 'PENDING' | 'CONFIRMED' | 'EXPIRED' | 'CANCELLED'
  expires_at: string
  seconds_left: number
  total_price_cents: number
  seats?: Seat[]
}

export interface Booking {
  id: string
  reservation_id: string
  session_id: string
  user_id: string
  booking_reference: string
  status: 'CONFIRMED' | 'CANCELLED'
  total_price_cents: number
  confirmed_at: string
  cancelled_at?: string
  seats?: Seat[]
  event_title?: string
  session_title?: string
  venue?: string
  start_time?: string
}

export interface Payment {
  id: string
  reservation_id: string
  booking_id?: string
  user_id: string
  amount_cents: number
  currency: string
  status: 'PENDING' | 'SUCCESS' | 'FAILED' | 'TIMEOUT'
  idempotency_key?: string
  provider_ref?: string
  error_message?: string
  created_at: string
}

export interface SystemStats {
  engine: string
  status: string
  cache_healthy: boolean
  message_bus_healthy: boolean
  total_events: number
  total_sessions: number
  total_seats: number
  total_bookings: number
  total_reservations: number
  active_seat_holds: number
  timestamp: string
}

export interface ConcurrencyTestResults {
  total_attempts: number
  successful_holds: number
  conflicts_prevented: number
  unexpected_errors: number
  winning_reservation: string
  duration_ms: number
  seat_final_status: string
  invariant_held: boolean
  double_booking_status: string
}
