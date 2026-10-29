import axios, { AxiosRequestConfig, AxiosResponse } from "axios";
import { apiClient, invalidateRequests, SESSION_ENDED } from "./httpClient";

test("discards an in-flight response after logout", async () => {
  let finish: (response: AxiosResponse) => void = () => {};
  let request: AxiosRequestConfig = {};
  let started: () => void = () => {};
  const ready = new Promise<void>((resolve) => {
    started = resolve;
  });
  apiClient.defaults.adapter = (config) => {
    request = config;
    started();
    return new Promise((resolve) => {
      finish = resolve;
    });
  };
  const pending = apiClient.get("/api/queues");
  await ready;
  invalidateRequests();
  finish({
    config: request,
    data: { private: true },
    status: 200,
    statusText: "OK",
    headers: {},
  });
  await expect(pending.catch((error) => axios.isCancel(error))).resolves.toBe(
    true
  );
});

test("notifies the UI when a protected request returns 401", async () => {
  window.AUTH_ENABLED = true;
  const ended = jest.fn();
  window.addEventListener(SESSION_ENDED, ended);
  apiClient.defaults.adapter = async (config) => {
    throw { config, response: { status: 401 } };
  };
  await expect(apiClient.get("/api/queues")).rejects.toMatchObject({
    response: { status: 401 },
  });
  expect(ended).toHaveBeenCalledTimes(1);
  window.removeEventListener(SESSION_ENDED, ended);
});
