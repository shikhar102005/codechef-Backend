import http from 'k6/http';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '10s', target: 20 },
    { duration: '20s', target: 40 },
    { duration: '10s', target: 0 },
  ],
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export default function () {
  const rand = Math.random();
  const sessionId = 'b0000000-0000-0000-0000-000000000001';

  const authHeaders = {
    headers: {
      'Content-Type': 'application/json',
      'Authorization': 'Bearer demo-alice',
    },
  };

  if (rand < 0.6) {
    // 60% Read-heavy traffic
    http.get(`${BASE_URL}/api/v1/events`);
    http.get(`${BASE_URL}/api/v1/sessions/${sessionId}/seats`);
  } else if (rand < 0.85) {
    // 25% Reservation & Status Polling
    const seatsRes = http.get(`${BASE_URL}/api/v1/sessions/${sessionId}/seats`);
    const seats = seatsRes.json().seats;
    if (seats && seats.length > 0) {
      const avail = seats.filter((s) => s.status === 'AVAILABLE');
      if (avail.length > 0) {
        http.post(
          `${BASE_URL}/api/v1/reservations`,
          JSON.stringify({ session_id: sessionId, seat_ids: [avail[0].id] }),
          authHeaders
        );
      }
    }
  } else {
    // 15% Booking History & System Health
    http.get(`${BASE_URL}/api/v1/bookings`, authHeaders);
    http.get(`${BASE_URL}/health`);
  }

  sleep(0.5);
}
