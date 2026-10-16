import { useState, useEffect } from 'react'
import {
  Ticket, Calendar, Clock, MapPin, CheckCircle2, AlertTriangle, ShieldCheck,
  RefreshCw, Trash2, ArrowRight, Play, Flame, Lock, ExternalLink, CreditCard
} from 'lucide-react'
import { api } from './api/client'
import type { Event, Session, Seat, Reservation, Booking, SystemStats, ConcurrencyTestResults, User } from './types/api'

export default function App() {
  const [currentUser, setCurrentUser] = useState<User | null>(null)

  const [activeTab, setActiveTab] = useState<'events' | 'seatmap' | 'checkout' | 'bookings' | 'dashboard'>('events')

  // Events & Sessions State
  const [events, setEvents] = useState<Event[]>([])
  const [selectedEvent, setSelectedEvent] = useState<Event | null>(null)
  const [selectedSession, setSelectedSession] = useState<Session | null>(null)

  // Seat Map State
  const [seats, setSeats] = useState<Seat[]>([])
  const [selectedSeatIds, setSelectedSeatIds] = useState<string[]>([])
  const [loadingSeats, setLoadingSeats] = useState(false)
  const [seatError, setSeatError] = useState<string | null>(null)

  // Reservation & Hold State
  const [activeReservation, setActiveReservation] = useState<Reservation | null>(null)
  const [countdown, setCountdown] = useState<number>(300)
  const [reserving, setReserving] = useState(false)

  // Payment Simulator State
  const [paymentSimulation, setPaymentSimulation] = useState<'SUCCESS' | 'FAILURE' | 'DELAYED' | 'TIMEOUT'>('SUCCESS')
  const [paymentStatusMessage, setPaymentStatusMessage] = useState<string | null>(null)
  const [paymentError, setPaymentError] = useState<string | null>(null)
  const [processingPayment, setProcessingPayment] = useState(false)
  const [lastConfirmedBooking, setLastConfirmedBooking] = useState<Booking | null>(null)

  // My Bookings State
  const [myBookings, setMyBookings] = useState<Booking[]>([])
  const [loadingBookings, setLoadingBookings] = useState(false)

  // Developer Dashboard State
  const [systemStats, setSystemStats] = useState<SystemStats | null>(null)
  const [concurrencyCount, setConcurrencyCount] = useState<number>(50)
  const [concurrencyRunning, setConcurrencyRunning] = useState(false)
  const [concurrencyResult, setConcurrencyResult] = useState<ConcurrencyTestResults | null>(null)

  // Initial Load & Auth Sync
  useEffect(() => {
    loginDefaultUser('alice@example.com', 'Password123!')
    loadEvents()
    loadSystemStats()
  }, [])

  // Poll active reservation countdown
  useEffect(() => {
    if (!activeReservation || activeReservation.status !== 'PENDING') return

    const timer = setInterval(() => {
      setCountdown((prev) => {
        if (prev <= 1) {
          clearInterval(timer)
          handleReservationExpired()
          return 0
        }
        return prev - 1
      })
    }, 1000)

    return () => clearInterval(timer)
  }, [activeReservation])

  const handleReservationExpired = () => {
    if (activeReservation) {
      setActiveReservation({ ...activeReservation, status: 'EXPIRED' })
      setPaymentError('Your temporary reservation hold has expired. The seats have been released back to availability.')
      if (selectedSession) {
        loadSeats(selectedSession.id)
      }
    }
  }

  const loginDefaultUser = async (email: string, pass: string) => {
    try {
      const res = await api.login(email, pass)
      setCurrentUser(res.user)
      loadMyBookings()
    } catch (err: any) {
      console.error('Login error:', err.message)
    }
  }

  const loadEvents = async () => {
    try {
      const res = await api.listEvents()
      setEvents(res.events || [])
      if (res.events && res.events.length > 0) {
        setSelectedEvent(res.events[0])
        if (res.events[0].sessions && res.events[0].sessions.length > 0) {
          setSelectedSession(res.events[0].sessions[0])
        }
      }
    } catch (err: any) {
      console.error('Failed to load events:', err)
    }
  }

  const loadSeats = async (sessionId: string) => {
    setLoadingSeats(true)
    setSeatError(null)
    try {
      const res = await api.getSessionSeats(sessionId)
      setSeats(res.seats || [])
    } catch (err: any) {
      setSeatError(err.message)
    } finally {
      setLoadingSeats(false)
    }
  }

  const selectEventAndSession = (event: Event, session: Session) => {
    setSelectedEvent(event)
    setSelectedSession(session)
    setSelectedSeatIds([])
    loadSeats(session.id)
    setActiveTab('seatmap')
  }

  const toggleSeatSelection = (seat: Seat) => {
    if (seat.status !== 'AVAILABLE') return

    setSelectedSeatIds((prev) => {
      if (prev.includes(seat.id)) {
        return prev.filter((id) => id !== seat.id)
      } else {
        if (prev.length >= 6) {
          alert('Maximum 6 seats per transaction')
          return prev
        }
        return [...prev, seat.id]
      }
    })
  }

  // Handle Reserve Seats
  const handleReserveSelectedSeats = async () => {
    if (!selectedSession || selectedSeatIds.length === 0) return

    setReserving(true)
    setSeatError(null)
    try {
      const idempotencyKey = `hold-${Date.now()}-${Math.random().toString(36).substring(7)}`
      const res = await api.createReservation(selectedSession.id, selectedSeatIds, idempotencyKey)
      setActiveReservation(res.reservation)
      setCountdown(res.reservation.seconds_left || 300)
      setActiveTab('checkout')
      // Refresh seatmap state
      loadSeats(selectedSession.id)
    } catch (err: any) {
      setSeatError(err.message)
      // Reload seats to show current conflict state
      if (selectedSession) loadSeats(selectedSession.id)
    } finally {
      setReserving(false)
    }
  }

  // Handle Payment
  const handleProcessPayment = async () => {
    if (!activeReservation) return

    setProcessingPayment(true)
    setPaymentError(null)
    setPaymentStatusMessage(null)

    try {
      const idempotencyKey = `pay-${activeReservation.id}`
      const res = await api.createPayment({
        reservationId: activeReservation.id,
        amountCents: activeReservation.total_price_cents,
        simulation: paymentSimulation,
        idempotencyKey,
      })

      if (res.booking) {
        setLastConfirmedBooking(res.booking)
        setActiveReservation(null)
        setPaymentStatusMessage('Payment confirmed! Booking successfully created.')
        loadMyBookings()
        if (selectedSession) loadSeats(selectedSession.id)
      } else if (paymentSimulation === 'DELAYED') {
        setPaymentStatusMessage('Asynchronous payment enqueued to RabbitMQ worker. Polling status...')
        // Poll status
        setTimeout(async () => {
          try {
            const check = await api.getPayment(res.payment.id)
            if (check.payment.status === 'SUCCESS' && check.payment.booking_id) {
              const b = await api.getBooking(check.payment.booking_id)
              setLastConfirmedBooking(b.booking)
              setActiveReservation(null)
              setPaymentStatusMessage('Asynchronous payment completed by background worker!')
              loadMyBookings()
              if (selectedSession) loadSeats(selectedSession.id)
            }
          } catch (e) {
            console.error('Polling error', e)
          }
        }, 3500)
      }
    } catch (err: any) {
      setPaymentError(err.message)
    } finally {
      setProcessingPayment(false)
    }
  }

  // Handle Cancel Active Reservation
  const handleCancelActiveReservation = async () => {
    if (!activeReservation) return
    try {
      await api.cancelReservation(activeReservation.id)
      setActiveReservation(null)
      setSelectedSeatIds([])
      if (selectedSession) loadSeats(selectedSession.id)
      setActiveTab('seatmap')
    } catch (err: any) {
      setPaymentError(err.message)
    }
  }

  // Load My Bookings
  const loadMyBookings = async () => {
    setLoadingBookings(true)
    try {
      const res = await api.listBookings()
      setMyBookings(res.bookings || [])
    } catch (err) {
      console.error('Failed to load bookings:', err)
    } finally {
      setLoadingBookings(false)
    }
  }

  // Cancel Booking
  const handleCancelBooking = async (bookingId: string) => {
    if (!confirm('Are you sure you want to cancel this booking? Confirmed seats will immediately be reallocated to the available pool.')) return

    try {
      await api.cancelBooking(bookingId)
      loadMyBookings()
      if (selectedSession) loadSeats(selectedSession.id)
      alert('Booking cancelled successfully and seats released.')
    } catch (err: any) {
      alert(`Cancel failed: ${err.message}`)
    }
  }

  // Developer Dashboard Stats
  const loadSystemStats = async () => {
    try {
      const res = await api.getSystemStats()
      setSystemStats(res.stats)
    } catch (err) {
      console.error('Failed to load system stats:', err)
    }
  }

  // Run Concurrency Stress Demo
  const handleRunConcurrencyStress = async () => {
    if (!selectedSession || seats.length === 0) {
      alert('Please select a session first')
      return
    }

    // Pick first available seat, or first seat
    const target = seats.find((s) => s.status === 'AVAILABLE') || seats[0]

    setConcurrencyRunning(true)
    setConcurrencyResult(null)
    try {
      const res = await api.runConcurrencyDemo(selectedSession.id, target.id, concurrencyCount)
      setConcurrencyResult(res.results)
      loadSeats(selectedSession.id)
      loadSystemStats()
    } catch (err: any) {
      alert(`Stress demo error: ${err.message}`)
    } finally {
      setConcurrencyRunning(false)
    }
  }

  const selectedSeatsList = seats.filter((s) => selectedSeatIds.includes(s.id))
  const totalPrice = selectedSeatsList.reduce((acc, s) => acc + s.base_price_cents, 0)

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex flex-col font-sans selection:bg-indigo-600 selection:text-white">
      {/* Top Navbar */}
      <header className="sticky top-0 z-50 glass-panel border-b border-slate-800/80 px-4 lg:px-8 py-3 flex flex-wrap items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          <div className="w-10 h-10 rounded-xl bg-gradient-to-tr from-indigo-600 to-violet-500 flex items-center justify-center shadow-lg shadow-indigo-500/20">
            <Ticket className="w-5 h-5 text-white" />
          </div>
          <div>
            <div className="flex items-center gap-2">
              <span className="font-bold text-lg tracking-tight bg-gradient-to-r from-white via-slate-200 to-indigo-300 bg-clip-text text-transparent">
                SeatMaster Pro
              </span>
              <span className="text-[10px] font-semibold px-2 py-0.5 rounded-full bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">
                PostgreSQL Row-Locked
              </span>
            </div>
            <p className="text-xs text-slate-400">Concurrency-Safe Seat Reservation Engine</p>
          </div>
        </div>

        {/* Navigation Tabs */}
        <nav className="flex items-center gap-1 bg-slate-900/80 p-1 rounded-xl border border-slate-800">
          <button
            onClick={() => setActiveTab('events')}
            className={`px-3 py-1.5 rounded-lg text-xs font-medium transition-all ${
              activeTab === 'events' ? 'bg-indigo-600 text-white shadow' : 'text-slate-400 hover:text-white'
            }`}
          >
            Events
          </button>
          <button
            onClick={() => {
              if (selectedSession) loadSeats(selectedSession.id)
              setActiveTab('seatmap')
            }}
            className={`px-3 py-1.5 rounded-lg text-xs font-medium transition-all ${
              activeTab === 'seatmap' ? 'bg-indigo-600 text-white shadow' : 'text-slate-400 hover:text-white'
            }`}
          >
            Seat Map
          </button>
          <button
            onClick={() => setActiveTab('checkout')}
            className={`px-3 py-1.5 rounded-lg text-xs font-medium transition-all relative ${
              activeTab === 'checkout' ? 'bg-indigo-600 text-white shadow' : 'text-slate-400 hover:text-white'
            }`}
          >
            Checkout
            {activeReservation && (
              <span className="absolute -top-1 -right-1 w-2.5 h-2.5 bg-amber-400 rounded-full animate-ping" />
            )}
          </button>
          <button
            onClick={() => {
              loadMyBookings()
              setActiveTab('bookings')
            }}
            className={`px-3 py-1.5 rounded-lg text-xs font-medium transition-all ${
              activeTab === 'bookings' ? 'bg-indigo-600 text-white shadow' : 'text-slate-400 hover:text-white'
            }`}
          >
            My Bookings
          </button>
          <button
            onClick={() => {
              loadSystemStats()
              setActiveTab('dashboard')
            }}
            className={`px-3 py-1.5 rounded-lg text-xs font-medium transition-all flex items-center gap-1.5 ${
              activeTab === 'dashboard' ? 'bg-indigo-600 text-white shadow' : 'text-slate-400 hover:text-white'
            }`}
          >
            <Flame className="w-3.5 h-3.5 text-amber-400" />
            Dev & Stress Demo
          </button>
        </nav>

        {/* User Switcher / Auth Bar */}
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-1.5 bg-slate-900 border border-slate-800 rounded-lg p-1 text-xs">
            <span className="text-slate-400 px-1.5 font-medium">Demo User:</span>
            <button
              onClick={() => loginDefaultUser('alice@example.com', 'Password123!')}
              className={`px-2 py-0.5 rounded text-[11px] font-semibold transition ${
                currentUser?.email === 'alice@example.com' ? 'bg-indigo-600 text-white' : 'text-slate-400 hover:text-white'
              }`}
            >
              Alice
            </button>
            <button
              onClick={() => loginDefaultUser('bob@example.com', 'Password123!')}
              className={`px-2 py-0.5 rounded text-[11px] font-semibold transition ${
                currentUser?.email === 'bob@example.com' ? 'bg-indigo-600 text-white' : 'text-slate-400 hover:text-white'
              }`}
            >
              Bob
            </button>
            <button
              onClick={() => loginDefaultUser('admin@example.com', 'AdminSecret123!')}
              className={`px-2 py-0.5 rounded text-[11px] font-semibold transition ${
                currentUser?.email === 'admin@example.com' ? 'bg-amber-600 text-white' : 'text-slate-400 hover:text-white'
              }`}
            >
              Admin
            </button>
          </div>

          <a
            href="/swagger"
            target="_blank"
            rel="noreferrer"
            className="flex items-center gap-1 text-xs text-indigo-400 hover:text-indigo-300 font-medium px-2 py-1 bg-indigo-950/40 rounded border border-indigo-800/40"
          >
            Swagger UI
            <ExternalLink className="w-3 h-3" />
          </a>
        </div>
      </header>

      {/* Main Content Area */}
      <main className="flex-1 p-4 lg:p-8 max-w-7xl w-full mx-auto">
        {/* Error Notification Bar */}
        {seatError && (
          <div className="mb-6 p-4 rounded-xl bg-rose-950/60 border border-rose-500/30 text-rose-200 flex items-center justify-between gap-4 animate-shake">
            <div className="flex items-center gap-3">
              <AlertTriangle className="w-5 h-5 text-rose-400 flex-shrink-0" />
              <p className="text-sm font-medium">{seatError}</p>
            </div>
            <button
              onClick={() => setSeatError(null)}
              className="text-xs bg-rose-900/60 hover:bg-rose-800 px-3 py-1 rounded text-white"
            >
              Dismiss
            </button>
          </div>
        )}

        {/* TAB 1: Events Explorer */}
        {activeTab === 'events' && (
          <div className="space-y-6">
            <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
              <div>
                <h1 className="text-2xl font-bold tracking-tight text-white">Upcoming Premier Events</h1>
                <p className="text-sm text-slate-400">Select an event and session to access the real-time seat reservation map.</p>
              </div>
              <button
                onClick={loadEvents}
                className="flex items-center gap-2 text-xs font-semibold px-3 py-2 bg-slate-900 hover:bg-slate-800 border border-slate-700 rounded-lg text-slate-300"
              >
                <RefreshCw className="w-3.5 h-3.5" />
                Refresh State
              </button>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
              {events.map((event) => (
                <div
                  key={event.id}
                  className="glass-card rounded-2xl p-5 border border-slate-800 hover:border-indigo-500/50 transition-all flex flex-col justify-between group shadow-lg hover:shadow-indigo-500/10"
                >
                  <div>
                    <div className="flex items-center justify-between mb-3">
                      <span className="text-[11px] font-bold tracking-wide uppercase px-2.5 py-1 rounded-full bg-indigo-500/10 text-indigo-400 border border-indigo-500/20">
                        {event.category}
                      </span>
                      <span className="text-xs text-slate-400 flex items-center gap-1">
                        <MapPin className="w-3.5 h-3.5 text-slate-400" />
                        {event.venue}
                      </span>
                    </div>

                    <h2 className="text-lg font-bold text-white mb-2 group-hover:text-indigo-400 transition-colors">
                      {event.title}
                    </h2>
                    <p className="text-xs text-slate-400 mb-4 line-clamp-2">{event.description}</p>

                    <div className="space-y-2 mb-4">
                      <span className="text-[11px] font-semibold text-slate-400 uppercase tracking-wider">
                        Available Sessions:
                      </span>
                      {event.sessions?.map((session) => (
                        <div
                          key={session.id}
                          onClick={() => selectEventAndSession(event, session)}
                          className="flex items-center justify-between p-2.5 rounded-xl bg-slate-900/80 hover:bg-indigo-950/60 border border-slate-800/90 hover:border-indigo-600/50 cursor-pointer transition"
                        >
                          <div>
                            <p className="text-xs font-semibold text-slate-200">{session.title}</p>
                            <p className="text-[11px] text-slate-400 flex items-center gap-1 mt-0.5">
                              <Calendar className="w-3 h-3 text-slate-400" />
                              {new Date(session.start_time).toLocaleDateString([], {
                                month: 'short',
                                day: 'numeric',
                                hour: '2-digit',
                                minute: '2-digit',
                              })}
                            </p>
                          </div>
                          <div className="text-right">
                            <span className="text-xs font-bold text-emerald-400">
                              {session.available_seats} / {session.total_seats}
                            </span>
                            <span className="block text-[10px] text-slate-400">seats left</span>
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>

                  <button
                    onClick={() => {
                      if (event.sessions && event.sessions.length > 0) {
                        selectEventAndSession(event, event.sessions[0])
                      }
                    }}
                    className="w-full mt-2 py-2 px-4 rounded-xl bg-gradient-to-r from-indigo-600 to-violet-600 hover:from-indigo-500 hover:to-violet-500 text-white font-medium text-xs flex items-center justify-center gap-2 shadow-md shadow-indigo-600/20"
                  >
                    Select Seats
                    <ArrowRight className="w-3.5 h-3.5" />
                  </button>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* TAB 2: Visual Seat Map */}
        {activeTab === 'seatmap' && (
          <div className="space-y-6">
            <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 glass-card p-4 rounded-2xl border border-slate-800">
              <div>
                <span className="text-xs font-semibold text-indigo-400 uppercase tracking-wider">
                  {selectedEvent?.title}
                </span>
                <h1 className="text-xl font-bold text-white flex items-center gap-2">
                  {selectedSession?.title}
                  <span className="text-xs font-normal text-slate-400">
                    ({selectedSession ? new Date(selectedSession.start_time).toLocaleString() : ''})
                  </span>
                </h1>
              </div>

              <div className="flex items-center gap-2">
                <button
                  onClick={() => selectedSession && loadSeats(selectedSession.id)}
                  disabled={loadingSeats}
                  className="flex items-center gap-1.5 text-xs font-medium px-3 py-1.5 bg-slate-900 hover:bg-slate-800 border border-slate-700 rounded-lg text-slate-300"
                >
                  <RefreshCw className={`w-3.5 h-3.5 ${loadingSeats ? 'animate-spin' : ''}`} />
                  Sync Availability
                </button>
              </div>
            </div>

            <div className="grid grid-cols-1 lg:grid-cols-4 gap-6">
              {/* Interactive Seat Canvas */}
              <div className="lg:col-span-3 glass-card p-6 rounded-2xl border border-slate-800 flex flex-col items-center">
                {/* Stage Indicator */}
                <div className="w-full max-w-xl mb-8">
                  <div className="h-2 rounded-full bg-gradient-to-r from-transparent via-indigo-500 to-transparent shadow-lg shadow-indigo-500/50" />
                  <p className="text-center text-[11px] font-semibold tracking-widest uppercase text-slate-400 mt-2">
                    STAGE / SCREEN AREA
                  </p>
                </div>

                {/* Seat Map Legend */}
                <div className="flex flex-wrap items-center justify-center gap-4 mb-8 text-xs">
                  <div className="flex items-center gap-1.5">
                    <span className="w-4 h-4 rounded-md bg-emerald-500/20 border border-emerald-500" />
                    <span className="text-slate-300">Available</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <span className="w-4 h-4 rounded-md bg-indigo-600 border border-indigo-400 shadow-sm shadow-indigo-500" />
                    <span className="text-slate-300">Selected</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <span className="w-4 h-4 rounded-md bg-amber-500/20 border border-amber-500/60" />
                    <span className="text-slate-300">Temporary Hold (5m)</span>
                  </div>
                  <div className="flex items-center gap-1.5">
                    <span className="w-4 h-4 rounded-md bg-slate-800 border border-slate-700 opacity-60" />
                    <span className="text-slate-400">Booked / Unavailable</span>
                  </div>
                </div>

                {/* Grid Rows */}
                {loadingSeats ? (
                  <div className="py-20 flex flex-col items-center gap-3">
                    <RefreshCw className="w-6 h-6 text-indigo-400 animate-spin" />
                    <p className="text-xs text-slate-400">Loading authoritative seat availability from backend...</p>
                  </div>
                ) : (
                  <div className="space-y-3.5 w-full max-w-2xl">
                    {['A', 'B', 'C', 'D', 'E'].map((rowLabel) => {
                      const rowSeats = seats.filter((s) => s.row_label === rowLabel)
                      const category = rowSeats[0]?.seat_category || ''
                      return (
                        <div key={rowLabel} className="flex items-center justify-center gap-2">
                          <span className="w-6 text-xs font-bold text-slate-400 text-center">{rowLabel}</span>
                          <span className="w-16 text-[10px] font-medium text-slate-400 truncate text-right mr-1">
                            {category}
                          </span>
                          <div className="flex flex-wrap items-center gap-1.5">
                            {rowSeats.map((seat) => {
                              const isSelected = selectedSeatIds.includes(seat.id)
                              let style = 'bg-emerald-500/20 border-emerald-500/60 text-emerald-300 hover:scale-110 hover:border-emerald-400 cursor-pointer'

                              if (seat.status === 'HELD') {
                                style = 'bg-amber-500/20 border-amber-500/50 text-amber-300 cursor-not-allowed opacity-90'
                              } else if (seat.status === 'BOOKED' || seat.status === 'UNAVAILABLE') {
                                style = 'bg-slate-800/80 border-slate-700 text-slate-400 cursor-not-allowed opacity-40'
                              }

                              if (isSelected) {
                                style = 'bg-indigo-600 border-indigo-400 text-white shadow-lg shadow-indigo-600/40 scale-110'
                              }

                              return (
                                <button
                                  key={seat.id}
                                  onClick={() => toggleSeatSelection(seat)}
                                  disabled={seat.status !== 'AVAILABLE'}
                                  title={`Row ${seat.row_label}-${seat.seat_number} (${seat.seat_category}) - $${(seat.base_price_cents / 100).toFixed(2)} [Status: ${seat.status}]`}
                                  className={`w-8 h-8 rounded-lg border text-[11px] font-semibold flex items-center justify-center transition-all duration-150 ${style}`}
                                >
                                  {seat.seat_number}
                                </button>
                              )
                            })}
                          </div>
                        </div>
                      )
                    })}
                  </div>
                )}
              </div>

              {/* Order Summary Sidebar */}
              <div className="glass-card p-6 rounded-2xl border border-slate-800 flex flex-col justify-between">
                <div>
                  <h2 className="text-base font-bold text-white mb-4">Reservation Cart</h2>

                  {selectedSeatsList.length === 0 ? (
                    <div className="py-12 text-center text-slate-400 text-xs">
                      <Ticket className="w-8 h-8 mx-auto mb-2 text-slate-400 opacity-60" />
                      Select up to 6 available seats on the map to begin reservation.
                    </div>
                  ) : (
                    <div className="space-y-3 mb-6">
                      {selectedSeatsList.map((s) => (
                        <div
                          key={s.id}
                          className="flex items-center justify-between p-2 rounded-lg bg-slate-900 border border-slate-800 text-xs"
                        >
                          <div>
                            <span className="font-bold text-indigo-300">
                              Row {s.row_label} - Seat {s.seat_number}
                            </span>
                            <span className="block text-[10px] text-slate-400">{s.seat_category}</span>
                          </div>
                          <span className="font-semibold text-slate-200">${(s.base_price_cents / 100).toFixed(2)}</span>
                        </div>
                      ))}

                      <div className="border-t border-slate-800 pt-3 flex items-center justify-between text-sm font-bold text-white">
                        <span>Total ({selectedSeatsList.length} seats):</span>
                        <span className="text-indigo-400">${(totalPrice / 100).toFixed(2)}</span>
                      </div>
                    </div>
                  )}
                </div>

                <div className="space-y-2">
                  <button
                    onClick={handleReserveSelectedSeats}
                    disabled={selectedSeatsList.length === 0 || reserving}
                    className="w-full py-2.5 px-4 rounded-xl bg-gradient-to-r from-indigo-600 to-violet-600 hover:from-indigo-500 hover:to-violet-500 disabled:opacity-40 disabled:cursor-not-allowed text-white font-medium text-xs flex items-center justify-center gap-2 shadow-lg shadow-indigo-600/20"
                  >
                    {reserving ? (
                      <>
                        <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                        Acquiring Row Locks...
                      </>
                    ) : (
                      <>
                        Lock Selected Seats (5m Hold)
                        <Lock className="w-3.5 h-3.5" />
                      </>
                    )}
                  </button>
                  <p className="text-[10px] text-center text-slate-400">
                    Database row locks guarantee zero double bookings during concurrent checkout.
                  </p>
                </div>
              </div>
            </div>
          </div>
        )}

        {/* TAB 3: Reservation Checkout & Payment Simulator */}
        {activeTab === 'checkout' && (
          <div className="max-w-3xl mx-auto space-y-6">
            {!activeReservation && !lastConfirmedBooking ? (
              <div className="glass-card p-12 rounded-2xl border border-slate-800 text-center space-y-4">
                <Ticket className="w-12 h-12 text-slate-400 mx-auto" />
                <h2 className="text-lg font-bold text-white">No Active Reservation</h2>
                <p className="text-xs text-slate-400 max-w-sm mx-auto">
                  Select available seats from the seat map to establish an authoritative temporary hold.
                </p>
                <button
                  onClick={() => setActiveTab('seatmap')}
                  className="px-4 py-2 bg-indigo-600 hover:bg-indigo-500 text-white text-xs font-semibold rounded-xl"
                >
                  Browse Seat Map
                </button>
              </div>
            ) : lastConfirmedBooking ? (
              /* Booking Confirmation View */
              <div className="glass-card p-8 rounded-2xl border border-emerald-500/30 space-y-6">
                <div className="flex items-center gap-3 text-emerald-400">
                  <CheckCircle2 className="w-8 h-8" />
                  <div>
                    <h2 className="text-xl font-bold text-white">Booking Confirmed!</h2>
                    <p className="text-xs text-emerald-300">Transaction completed and seats permanently committed.</p>
                  </div>
                </div>

                <div className="bg-slate-900/90 border border-slate-800 rounded-xl p-5 space-y-4">
                  <div className="flex items-center justify-between border-b border-slate-800 pb-3">
                    <span className="text-xs text-slate-400">Booking Reference</span>
                    <span className="font-mono font-bold text-sm text-indigo-400 tracking-wider">
                      {lastConfirmedBooking.booking_reference}
                    </span>
                  </div>
                  <div className="flex items-center justify-between border-b border-slate-800 pb-3">
                    <span className="text-xs text-slate-400">Event / Session</span>
                    <span className="text-xs font-semibold text-white">
                      {lastConfirmedBooking.event_title || selectedEvent?.title} - {lastConfirmedBooking.session_title || selectedSession?.title}
                    </span>
                  </div>
                  <div className="flex items-center justify-between border-b border-slate-800 pb-3">
                    <span className="text-xs text-slate-400">Confirmed Seats</span>
                    <span className="text-xs font-bold text-emerald-400">
                      {lastConfirmedBooking.seats?.map((s) => `${s.row_label}${s.seat_number}`).join(', ') || 'Seats Confirmed'}
                    </span>
                  </div>
                  <div className="flex items-center justify-between">
                    <span className="text-xs text-slate-400">Total Charged</span>
                    <span className="text-sm font-bold text-white">
                      ${(lastConfirmedBooking.total_price_cents / 100).toFixed(2)} USD
                    </span>
                  </div>
                </div>

                <div className="flex items-center gap-3">
                  <button
                    onClick={() => {
                      setLastConfirmedBooking(null)
                      loadMyBookings()
                      setActiveTab('bookings')
                    }}
                    className="flex-1 py-2.5 px-4 bg-indigo-600 hover:bg-indigo-500 text-white text-xs font-semibold rounded-xl text-center"
                  >
                    View in My Bookings
                  </button>
                  <button
                    onClick={() => {
                      setLastConfirmedBooking(null)
                      setActiveTab('events')
                    }}
                    className="py-2.5 px-4 bg-slate-900 hover:bg-slate-800 border border-slate-700 text-slate-300 text-xs font-semibold rounded-xl"
                  >
                    Book Another Event
                  </button>
                </div>
              </div>
            ) : (
              /* Active Reservation & Simulator View */
              <div className="space-y-6">
                {/* Hold Banner with Countdown Clock */}
                <div className="glass-card p-5 rounded-2xl border border-amber-500/40 bg-amber-950/10 flex flex-wrap items-center justify-between gap-4">
                  <div className="flex items-center gap-3">
                    <Clock className="w-6 h-6 text-amber-400 animate-pulse" />
                    <div>
                      <span className="text-[11px] font-bold uppercase tracking-wider text-amber-400">
                        Temporary Hold Active
                      </span>
                      <p className="text-xs text-slate-300">
                        Reservation ID: <span className="font-mono text-slate-400">{activeReservation?.id.substring(0, 13)}...</span>
                      </p>
                    </div>
                  </div>

                  <div className="text-right">
                    <span className="text-2xl font-mono font-bold text-amber-400">
                      {Math.floor(countdown / 60)}:{(countdown % 60).toString().padStart(2, '0')}
                    </span>
                    <span className="block text-[10px] text-slate-400">until seats released</span>
                  </div>
                </div>

                {/* Payment Simulator Panel */}
                <div className="glass-card p-6 rounded-2xl border border-slate-800 space-y-5">
                  <div className="flex items-center justify-between">
                    <div>
                      <h2 className="text-base font-bold text-white flex items-center gap-2">
                        <CreditCard className="w-5 h-5 text-indigo-400" />
                        Simulated Payment Gateway
                      </h2>
                      <p className="text-xs text-slate-400">
                        Evaluator controls to test transactional state machine outcomes deterministically.
                      </p>
                    </div>
                    <span className="text-[10px] font-bold px-2 py-0.5 rounded bg-indigo-500/10 text-indigo-400 border border-indigo-500/20">
                      Demo Mode
                    </span>
                  </div>

                  {paymentStatusMessage && (
                    <div className="p-3 rounded-xl bg-emerald-950/60 border border-emerald-500/30 text-emerald-200 text-xs">
                      {paymentStatusMessage}
                    </div>
                  )}

                  {paymentError && (
                    <div className="p-3 rounded-xl bg-rose-950/60 border border-rose-500/30 text-rose-200 text-xs">
                      {paymentError}
                    </div>
                  )}

                  {/* Simulator Mode Selector */}
                  <div className="grid grid-cols-2 sm:grid-cols-4 gap-2.5">
                    {[
                      { id: 'SUCCESS', label: 'SUCCESS (200)', desc: 'Commits booking' },
                      { id: 'FAILURE', label: 'FAILURE (402)', desc: 'Card declined' },
                      { id: 'DELAYED', label: 'DELAYED (202)', desc: 'RabbitMQ queue' },
                      { id: 'TIMEOUT', label: 'TIMEOUT (504)', desc: 'Idempotent retry' },
                    ].map((mode) => (
                      <button
                        key={mode.id}
                        type="button"
                        onClick={() => setPaymentSimulation(mode.id as any)}
                        className={`p-3 rounded-xl border text-left transition ${
                          paymentSimulation === mode.id
                            ? 'bg-indigo-600/20 border-indigo-500 text-white ring-1 ring-indigo-500'
                            : 'bg-slate-900 border-slate-800 text-slate-400 hover:border-slate-700'
                        }`}
                      >
                        <span className="block text-xs font-bold text-slate-200">{mode.label}</span>
                        <span className="block text-[10px] text-slate-400 mt-0.5">{mode.desc}</span>
                      </button>
                    ))}
                  </div>

                  {/* Pricing Breakdown */}
                  <div className="p-4 rounded-xl bg-slate-900 border border-slate-800 space-y-2">
                    <div className="flex justify-between text-xs text-slate-400">
                      <span>Held Seats:</span>
                      <span className="font-bold text-slate-200">
                        {activeReservation?.seats?.map((s) => `${s.row_label}${s.seat_number}`).join(', ') || 'Selected Seats'}
                      </span>
                    </div>
                    <div className="flex justify-between text-xs text-slate-400">
                      <span>Total Amount:</span>
                      <span className="font-bold text-lg text-emerald-400">
                        ${((activeReservation?.total_price_cents || 0) / 100).toFixed(2)} USD
                      </span>
                    </div>
                  </div>

                  {/* Action Buttons */}
                  <div className="flex items-center gap-3">
                    <button
                      onClick={handleProcessPayment}
                      disabled={processingPayment || countdown <= 0}
                      className="flex-1 py-3 px-4 rounded-xl bg-gradient-to-r from-emerald-600 to-teal-600 hover:from-emerald-500 hover:to-teal-500 disabled:opacity-40 disabled:cursor-not-allowed text-white font-semibold text-xs flex items-center justify-center gap-2 shadow-lg shadow-emerald-600/20"
                    >
                      {processingPayment ? (
                        <>
                          <RefreshCw className="w-4 h-4 animate-spin" />
                          Processing Transaction...
                        </>
                      ) : (
                        <>
                          Execute Simulated Payment ({paymentSimulation})
                          <ArrowRight className="w-4 h-4" />
                        </>
                      )}
                    </button>
                    <button
                      onClick={handleCancelActiveReservation}
                      className="py-3 px-4 rounded-xl bg-slate-900 hover:bg-slate-800 border border-slate-700 text-rose-400 hover:text-rose-300 text-xs font-semibold"
                    >
                      Release Seats
                    </button>
                  </div>
                </div>
              </div>
            )}
          </div>
        )}

        {/* TAB 4: My Bookings */}
        {activeTab === 'bookings' && (
          <div className="space-y-6">
            <div className="flex items-center justify-between">
              <div>
                <h1 className="text-2xl font-bold text-white">My Booking History</h1>
                <p className="text-sm text-slate-400">
                  Signed in as <span className="font-semibold text-slate-200">{currentUser?.full_name}</span> ({currentUser?.email})
                </p>
              </div>
              <button
                onClick={loadMyBookings}
                className="flex items-center gap-1.5 text-xs font-semibold px-3 py-2 bg-slate-900 hover:bg-slate-800 border border-slate-700 rounded-lg text-slate-300"
              >
                <RefreshCw className="w-3.5 h-3.5" />
                Refresh
              </button>
            </div>

            {loadingBookings ? (
              <div className="py-20 text-center">
                <RefreshCw className="w-6 h-6 text-indigo-400 animate-spin mx-auto mb-2" />
                <p className="text-xs text-slate-400">Fetching user bookings from PostgreSQL...</p>
              </div>
            ) : myBookings.length === 0 ? (
              <div className="glass-card p-12 rounded-2xl border border-slate-800 text-center space-y-3">
                <Ticket className="w-10 h-10 text-slate-400 mx-auto opacity-40" />
                <h2 className="text-base font-bold text-white">No Bookings Yet</h2>
                <p className="text-xs text-slate-400">
                  You have not confirmed any bookings under this account yet.
                </p>
                <button
                  onClick={() => setActiveTab('events')}
                  className="px-4 py-2 bg-indigo-600 hover:bg-indigo-500 text-white text-xs font-semibold rounded-xl"
                >
                  Explore Events
                </button>
              </div>
            ) : (
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                {myBookings.map((b) => (
                  <div key={b.id} className="glass-card p-5 rounded-2xl border border-slate-800 space-y-4">
                    <div className="flex items-start justify-between">
                      <div>
                        <span className="font-mono text-xs font-bold text-indigo-400 tracking-wider">
                          {b.booking_reference}
                        </span>
                        <h2 className="text-sm font-bold text-white mt-1">{b.event_title}</h2>
                        <p className="text-xs text-slate-400">{b.session_title}</p>
                      </div>
                      <span
                        className={`text-[10px] font-bold px-2 py-0.5 rounded-full ${
                          b.status === 'CONFIRMED'
                            ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                            : 'bg-rose-500/10 text-rose-400 border border-rose-500/20'
                        }`}
                      >
                        {b.status}
                      </span>
                    </div>

                    <div className="border-t border-slate-800/80 pt-3 flex items-center justify-between text-xs">
                      <span className="text-slate-400">Seats:</span>
                      <span className="font-bold text-slate-200">
                        {b.seats?.map((s) => `${s.row_label}${s.seat_number}`).join(', ') || 'Assigned Seats'}
                      </span>
                    </div>

                    <div className="flex items-center justify-between text-xs">
                      <span className="text-slate-400">Total Price:</span>
                      <span className="font-bold text-white">${(b.total_price_cents / 100).toFixed(2)}</span>
                    </div>

                    {b.status === 'CONFIRMED' && (
                      <button
                        onClick={() => handleCancelBooking(b.id)}
                        className="w-full mt-2 py-2 px-3 bg-slate-900 hover:bg-rose-950/60 border border-slate-800 hover:border-rose-500/40 text-rose-400 hover:text-rose-300 text-xs font-medium rounded-xl transition flex items-center justify-center gap-1.5"
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                        Cancel Booking & Release Seats
                      </button>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        {/* TAB 5: Developer Dashboard & Live Concurrency Stress Demo */}
        {activeTab === 'dashboard' && (
          <div className="space-y-8">
            <div>
              <h1 className="text-2xl font-bold text-white flex items-center gap-2">
                <Flame className="w-6 h-6 text-amber-400" />
                Distributed Systems & Concurrency Audit Dashboard
              </h1>
              <p className="text-sm text-slate-400">
                Live inspection of system dependencies, database row locks, and real-time concurrency stress verification.
              </p>
            </div>

            {/* Architecture Health Cards */}
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
              <div className="glass-card p-4 rounded-2xl border border-slate-800">
                <div className="flex items-center justify-between mb-2">
                  <span className="text-xs text-slate-400">Database Engine</span>
                  <span className="w-2.5 h-2.5 rounded-full bg-emerald-400" />
                </div>
                <p className="text-base font-bold text-white uppercase">{systemStats?.engine || 'PostgreSQL'}</p>
                <p className="text-[11px] text-slate-400 mt-1">Authoritative Source of Truth</p>
              </div>

              <div className="glass-card p-4 rounded-2xl border border-slate-800">
                <div className="flex items-center justify-between mb-2">
                  <span className="text-xs text-slate-400">Redis Cache & Rate Limiter</span>
                  <span
                    className={`w-2.5 h-2.5 rounded-full ${
                      systemStats?.cache_healthy ? 'bg-emerald-400' : 'bg-amber-400'
                    }`}
                  />
                </div>
                <p className="text-base font-bold text-white">
                  {systemStats?.cache_healthy ? 'Operational' : 'Resilient Fallback'}
                </p>
                <p className="text-[11px] text-slate-400 mt-1">TTL Invalidation + Sliding Window</p>
              </div>

              <div className="glass-card p-4 rounded-2xl border border-slate-800">
                <div className="flex items-center justify-between mb-2">
                  <span className="text-xs text-slate-400">RabbitMQ Message Bus</span>
                  <span
                    className={`w-2.5 h-2.5 rounded-full ${
                      systemStats?.message_bus_healthy ? 'bg-emerald-400' : 'bg-emerald-400'
                    }`}
                  />
                </div>
                <p className="text-base font-bold text-white">
                  {systemStats?.message_bus_healthy ? 'Connected' : 'In-Process Bus'}
                </p>
                <p className="text-[11px] text-slate-400 mt-1">Transactional Outbox Processor</p>
              </div>

              <div className="glass-card p-4 rounded-2xl border border-slate-800">
                <div className="flex items-center justify-between mb-2">
                  <span className="text-xs text-slate-400">Active Seat Holds</span>
                  <span className="w-2.5 h-2.5 rounded-full bg-indigo-400" />
                </div>
                <p className="text-base font-bold text-white">{systemStats?.active_seat_holds || 0}</p>
                <p className="text-[11px] text-slate-400 mt-1">5-Minute Reconciled TTLs</p>
              </div>
            </div>

            {/* Interactive Concurrency Stress Demonstration */}
            <div className="glass-card p-6 rounded-2xl border border-indigo-500/30 space-y-6">
              <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                <div>
                  <h2 className="text-lg font-bold text-white flex items-center gap-2">
                    <ShieldCheck className="w-5 h-5 text-indigo-400" />
                    Simultaneous Users Race-Condition Stress Demo
                  </h2>
                  <p className="text-xs text-slate-400">
                    Triggers 10 to 100 simultaneous concurrent goroutines targeting the exact same seat in a single burst.
                  </p>
                </div>

                {/* Worker Count Selection */}
                <div className="flex items-center gap-2 bg-slate-900 border border-slate-800 p-1 rounded-xl">
                  {[10, 25, 50, 100].map((cnt) => (
                    <button
                      key={cnt}
                      onClick={() => setConcurrencyCount(cnt)}
                      className={`px-3 py-1 rounded-lg text-xs font-bold transition ${
                        concurrencyCount === cnt ? 'bg-indigo-600 text-white' : 'text-slate-400 hover:text-white'
                      }`}
                    >
                      {cnt} Workers
                    </button>
                  ))}
                </div>
              </div>

              <div className="flex items-center gap-4">
                <button
                  onClick={handleRunConcurrencyStress}
                  disabled={concurrencyRunning}
                  className="py-3 px-6 bg-gradient-to-r from-indigo-600 to-violet-600 hover:from-indigo-500 hover:to-violet-500 disabled:opacity-40 text-white font-bold text-xs rounded-xl flex items-center gap-2 shadow-lg shadow-indigo-600/20"
                >
                  {concurrencyRunning ? (
                    <>
                      <RefreshCw className="w-4 h-4 animate-spin" />
                      Firing {concurrencyCount} Goroutines Concurrently...
                    </>
                  ) : (
                    <>
                      <Play className="w-4 h-4 fill-white" />
                      Run Live Concurrency Stress Test ({concurrencyCount} Users)
                    </>
                  )}
                </button>
              </div>

              {/* Stress Results Display */}
              {concurrencyResult && (
                <div className="bg-slate-900/90 border border-slate-800 rounded-xl p-5 space-y-4 animate-fade-in">
                  <div className="flex items-center justify-between border-b border-slate-800 pb-3">
                    <span className="text-xs font-bold text-slate-300">Stress Test Outcome</span>
                    <span className="text-xs font-mono font-bold text-emerald-400 bg-emerald-500/10 px-2.5 py-1 rounded-full border border-emerald-500/20">
                      {concurrencyResult.double_booking_status}
                    </span>
                  </div>

                  <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 text-center">
                    <div className="p-3 rounded-lg bg-slate-950 border border-slate-800">
                      <span className="text-[10px] text-slate-400 uppercase font-semibold">Total Attempts</span>
                      <p className="text-xl font-bold text-white mt-1">{concurrencyResult.total_attempts}</p>
                    </div>
                    <div className="p-3 rounded-lg bg-slate-950 border border-slate-800">
                      <span className="text-[10px] text-slate-400 uppercase font-semibold">Successful Holds</span>
                      <p className="text-xl font-bold text-emerald-400 mt-1">{concurrencyResult.successful_holds}</p>
                    </div>
                    <div className="p-3 rounded-lg bg-slate-950 border border-slate-800">
                      <span className="text-[10px] text-slate-400 uppercase font-semibold">Conflicts Prevented (409)</span>
                      <p className="text-xl font-bold text-amber-400 mt-1">{concurrencyResult.conflicts_prevented}</p>
                    </div>
                    <div className="p-3 rounded-lg bg-slate-950 border border-slate-800">
                      <span className="text-[10px] text-slate-400 uppercase font-semibold">Duration (ms)</span>
                      <p className="text-xl font-bold text-indigo-400 mt-1">{concurrencyResult.duration_ms} ms</p>
                    </div>
                  </div>

                  <div className="p-3 rounded-lg bg-slate-950 border border-slate-800 flex items-center justify-between text-xs">
                    <span className="text-slate-400">Winning Reservation ID:</span>
                    <span className="font-mono text-emerald-300">{concurrencyResult.winning_reservation}</span>
                  </div>
                </div>
              )}
            </div>
          </div>
        )}
      </main>

      {/* Footer */}
      <footer className="border-t border-slate-900 py-6 px-4 text-center text-xs text-slate-400">
        <p>Seat Reservation & Booking System • Go, PostgreSQL (Supabase), Redis, RabbitMQ & React</p>
      </footer>
    </div>
  )
}
