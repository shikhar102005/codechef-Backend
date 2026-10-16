import type { User, Event, Session, Seat, Reservation, Booking, Payment, SystemStats, ConcurrencyTestResults } from '../types/api'

const API_BASE = '' // Uses Vite proxy in development, or reverse proxy in docker/nginx

class ApiClient {
  private token: string | null = null

  constructor() {
    this.token = localStorage.getItem('token')
  }

  setToken(token: string | null) {
    this.token = token
    if (token) {
      localStorage.setItem('token', token)
    } else {
      localStorage.removeItem('token')
    }
  }

  getToken(): string | null {
    return this.token
  }

  private async request<T>(path: string, options: RequestInit = {}): Promise<T> {
    const headers = new Headers(options.headers || {})
    headers.set('Content-Type', 'application/json')
    if (this.token) {
      headers.set('Authorization', `Bearer ${this.token}`)
    }

    const response = await fetch(`${API_BASE}${path}`, {
      ...options,
      headers,
    })

    const contentType = response.headers.get('content-type')
    const isJson = contentType && contentType.includes('application/json')
    const data = isJson ? await response.json() : await response.text()

    if (!response.ok) {
      const errorMsg = isJson && data.message ? data.message : (isJson && data.error ? data.error : `HTTP ${response.status}`)
      throw new Error(errorMsg)
    }

    return data as T
  }

  // Auth Endpoints
  async login(email: string, password: string): Promise<{ token: string; user: User }> {
    const res = await this.request<{ token: string; user: User }>('/api/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email, password }),
    })
    this.setToken(res.token)
    return res
  }

  async getMe(): Promise<{ user: User }> {
    return this.request<{ user: User }>('/api/v1/auth/me')
  }

  // Events & Sessions
  async listEvents(): Promise<{ events: Event[] }> {
    return this.request<{ events: Event[] }>('/api/v1/events')
  }

  async getEvent(id: string): Promise<{ event: Event }> {
    return this.request<{ event: Event }>(`/api/v1/events/${id}`)
  }

  async getSession(id: string): Promise<{ session: Session }> {
    return this.request<{ session: Session }>(`/api/v1/sessions/${id}`)
  }

  async getSessionSeats(sessionId: string): Promise<{ seats: Seat[]; total: number; available: number }> {
    return this.request<{ seats: Seat[]; total: number; available: number }>(`/api/v1/sessions/${sessionId}/seats`)
  }

  // Reservations
  async createReservation(sessionId: string, seatIds: string[], idempotencyKey?: string): Promise<{ reservation: Reservation; message: string }> {
    const headers: Record<string, string> = {}
    if (idempotencyKey) {
      headers['Idempotency-Key'] = idempotencyKey
    }

    return this.request<{ reservation: Reservation; message: string }>('/api/v1/reservations', {
      method: 'POST',
      headers,
      body: JSON.stringify({
        session_id: sessionId,
        seat_ids: seatIds,
      }),
    })
  }

  async getReservation(reservationId: string): Promise<{ reservation: Reservation }> {
    return this.request<{ reservation: Reservation }>(`/api/v1/reservations/${reservationId}`)
  }

  async confirmReservation(reservationId: string): Promise<{ booking: Booking; message: string }> {
    return this.request<{ booking: Booking; message: string }>(`/api/v1/reservations/${reservationId}/confirm`, {
      method: 'POST',
    })
  }

  async cancelReservation(reservationId: string): Promise<{ message: string }> {
    return this.request<{ message: string }>(`/api/v1/reservations/${reservationId}/cancel`, {
      method: 'POST',
    })
  }

  // Bookings
  async listBookings(): Promise<{ bookings: Booking[]; total: number }> {
    return this.request<{ bookings: Booking[]; total: number }>('/api/v1/bookings')
  }

  async getBooking(bookingId: string): Promise<{ booking: Booking }> {
    return this.request<{ booking: Booking }>(`/api/v1/bookings/${bookingId}`)
  }

  async cancelBooking(bookingId: string): Promise<{ message: string }> {
    return this.request<{ message: string }>(`/api/v1/bookings/${bookingId}/cancel`, {
      method: 'POST',
    })
  }

  // Payments
  async createPayment(params: {
    reservationId: string
    amountCents: number
    currency?: string
    simulation?: 'SUCCESS' | 'FAILURE' | 'DELAYED' | 'TIMEOUT'
    idempotencyKey?: string
  }): Promise<{ payment: Payment; booking?: Booking; message: string }> {
    const headers: Record<string, string> = {}
    if (params.idempotencyKey) {
      headers['Idempotency-Key'] = params.idempotencyKey
    }

    return this.request<{ payment: Payment; booking?: Booking; message: string }>('/api/v1/payments', {
      method: 'POST',
      headers,
      body: JSON.stringify({
        reservation_id: params.reservationId,
        amount_cents: params.amountCents,
        currency: params.currency || 'USD',
        simulation: params.simulation || 'SUCCESS',
      }),
    })
  }

  async getPayment(paymentId: string): Promise<{ payment: Payment }> {
    return this.request<{ payment: Payment }>(`/api/v1/payments/${paymentId}`)
  }

  // System & Concurrency Demo
  async getSystemStats(): Promise<{ stats: SystemStats }> {
    return this.request<{ stats: SystemStats }>('/api/v1/system/stats')
  }

  async runConcurrencyDemo(sessionId: string, seatId: string, count: number): Promise<{ results: ConcurrencyTestResults }> {
    return this.request<{ results: ConcurrencyTestResults }>('/api/v1/demo/concurrency-test', {
      method: 'POST',
      body: JSON.stringify({
        session_id: sessionId,
        seat_id: seatId,
        count,
      }),
    })
  }
}

export const api = new ApiClient()
