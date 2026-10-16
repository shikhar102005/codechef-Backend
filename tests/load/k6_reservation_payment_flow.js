import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  vus: 10,
  duration: '20s',
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export default function () {
  const sessionId = 'b0000000-0000-0000-0000-000000000001';

  // 1. View seats
  const seatsRes = http.get(`${BASE_URL}/api/v1/sessions/${sessionId}/seats`);
  const seats = seatsRes.json().seats;
  if (!seats || seats.length === 0) return;

  const availableSeats = seats.filter((s) => s.status === 'AVAILABLE');
  if (availableSeats.length === 0) return;

  const seatToBook = availableSeats[Math.floor(Math.random() * availableSeats.length)];

  // 2. Reserve Seat (Temporary Hold)
  const holdPayload = JSON.stringify({
    session_id: sessionId,
    seat_ids: [seatToBook.id],
  });

  const authHeaders = {
    headers: {
      'Content-Type': 'application/json',
      'Authorization': 'Bearer demo-alice',
    },
  };

  const holdRes = http.post(`${BASE_URL}/api/v1/reservations`, holdPayload, authHeaders);
  if (holdRes.status !== 201) {
    return; // Lost race to another user, retry next cycle
  }

  const reservationId = holdRes.json().reservation.id;

  sleep(1);

  // 3. Confirm through Payment Simulator
  const payPayload = JSON.stringify({
    reservation_id: reservationId,
    amount_cents: seatToBook.base_price_cents,
    simulation: 'SUCCESS',
  });

  const payRes = http.post(`${BASE_URL}/api/v1/payments`, payPayload, authHeaders);
  check(payRes, {
    'payment status is 200': (r) => r.status === 200,
    'booking created': (r) => r.json().booking !== undefined,
  });

  sleep(1);
}
