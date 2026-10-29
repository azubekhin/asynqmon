import axios, { AxiosRequestConfig } from "axios";

interface SessionRequestConfig extends AxiosRequestConfig {
  sessionGeneration?: number;
}

export const SESSION_ENDED = "asynqmon:session-ended";
let generation = 0;

// Invalidate responses already in flight when a user logs out or a session expires.
export function invalidateRequests() {
  generation += 1;
}

export const apiClient = axios.create();
apiClient.interceptors.request.use((config) => {
  config.headers = { ...config.headers, "X-Asynqmon-Request": "1" };
  (config as SessionRequestConfig).sessionGeneration = generation;
  return config;
});
apiClient.interceptors.response.use(
  (response) => {
    if (
      (response.config as SessionRequestConfig).sessionGeneration !== generation
    ) {
      return Promise.reject(new axios.Cancel("Session ended"));
    }
    return response;
  },
  (error) => {
    if (
      (error.config as SessionRequestConfig)?.sessionGeneration !== generation
    ) {
      return Promise.reject(new axios.Cancel("Session ended"));
    }
    if (error.response?.status === 401 && window.AUTH_ENABLED) {
      invalidateRequests();
      window.dispatchEvent(new Event(SESSION_ENDED));
    }
    return Promise.reject(error);
  }
);

// Login errors belong to the form, not the global expired-session handler.
export const authClient = axios.create({
  headers: { "X-Asynqmon-Request": "1" },
});
