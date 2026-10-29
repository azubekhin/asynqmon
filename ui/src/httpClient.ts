import axios from "axios";

export const SESSION_ENDED = "asynqmon:session-ended";
let pending = axios.CancelToken.source();

export const apiClient = axios.create({
  headers: { "X-Asynqmon-Request": "1" },
  cancelToken: pending.token,
});

// Cancel the old session's requests before allowing requests for a new session.
export function invalidateRequests() {
  pending.cancel("Session ended");
  pending = axios.CancelToken.source();
  apiClient.defaults.cancelToken = pending.token;
}

apiClient.interceptors.response.use(undefined, (error) => {
  if (error.response?.status === 401 && window.AUTH_ENABLED) {
    invalidateRequests();
    window.dispatchEvent(new Event(SESSION_ENDED));
  }
  return Promise.reject(error);
});

// Login errors belong to the form, not the expired-session handler.
export const authClient = axios.create({
  headers: { "X-Asynqmon-Request": "1" },
});
