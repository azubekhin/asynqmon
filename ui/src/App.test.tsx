import React from "react";
import { Provider } from "react-redux";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import App from "./App";
import store from "./store";
import * as auth from "./auth";
import { apiClient, SESSION_ENDED } from "./httpClient";

jest.mock("./auth", () => ({
  session: jest.fn(),
  login: jest.fn(),
  logout: jest.fn(),
}));
jest.mock("./components/SyntaxHighlighter", () => () => null);
jest.mock("./views/SettingsView", () => () => <div>Settings</div>);
jest.mock("./views/DashboardView", () => {
  const React = require("react");
  const { apiClient } = require("./httpClient");
  return function MockDashboard() {
    React.useEffect(() => {
      apiClient.get("/api/queues").catch(() => {});
    }, []);
    return <div>Protected queues</div>;
  };
});

const session = auth.session as jest.MockedFunction<typeof auth.session>;
const login = auth.login as jest.MockedFunction<typeof auth.login>;
const logout = auth.logout as jest.MockedFunction<typeof auth.logout>;
let requests: string[];

beforeEach(() => {
  jest.clearAllMocks();
  requests = [];
  window.AUTH_ENABLED = true;
  window.ROOT_PATH = "";
  window.READ_ONLY = false;
  window.history.replaceState({}, "", "/");
  store.dispatch({ type: "AUTH_SESSION_STARTED" });
  apiClient.defaults.adapter = async (config) => {
    requests.push(config.url || "");
    return { data: {}, status: 200, statusText: "OK", headers: {}, config };
  };
  session.mockResolvedValue(false);
  login.mockResolvedValue();
  logout.mockResolvedValue();
});

function mount() {
  return render(
    <Provider store={store}>
      <App />
    </Provider>
  );
}

async function signIn() {
  fireEvent.change(await screen.findByLabelText(/Username/), {
    target: { value: "admin" },
  });
  fireEvent.change(screen.getByLabelText(/Password/), {
    target: { value: "secret" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Sign in" }));
}

test("waits for session before mounting protected screens or requesting queues", async () => {
  let resolve: (value: boolean) => void = () => {};
  session.mockReturnValue(
    new Promise<boolean>((done) => {
      resolve = done;
    })
  );
  mount();
  expect(screen.getByLabelText("Checking session")).toBeInTheDocument();
  expect(requests).toEqual([]);
  await act(async () => {
    resolve(false);
  });
  expect(
    await screen.findByRole("button", { name: "Sign in" })
  ).toBeInTheDocument();
  expect(screen.queryByText("Protected queues")).not.toBeInTheDocument();
  expect(requests).toEqual([]);
});

test("logs in and logs out, clearing protected state", async () => {
  mount();
  await signIn();
  expect(await screen.findByText("Protected queues")).toBeInTheDocument();
  expect(login).toHaveBeenCalledWith("admin", "secret");
  await waitFor(() => expect(requests).toContain("/api/queues"));
  store.dispatch({ type: "LIST_QUEUES_ERROR", error: "private details" });
  fireEvent.click(screen.getByRole("button", { name: "Sign out" }));
  expect(
    await screen.findByRole("button", { name: "Sign in" })
  ).toBeInTheDocument();
  expect(logout).toHaveBeenCalledTimes(1);
  expect(screen.queryByText("Protected queues")).not.toBeInTheDocument();
  expect(JSON.stringify(store.getState())).not.toContain("private details");
});

test.each([401, 429, 500])(
  "shows login error %s and clears password",
  async (status) => {
    login.mockRejectedValue({ response: { status } });
    mount();
    await signIn();
    expect(await screen.findByRole("alert")).toHaveTextContent(
      status === 401
        ? "Invalid username or password"
        : status === 429
        ? "Too many login attempts"
        : "Could not sign in"
    );
    expect(screen.getByLabelText(/Password/)).toHaveValue("");
    expect(requests).toEqual([]);
  }
);

test("restores an existing session", async () => {
  session.mockResolvedValue(true);
  mount();
  expect(await screen.findByText("Protected queues")).toBeInTheDocument();
  expect(login).not.toHaveBeenCalled();
});

test("returns to login after a protected API returns 401", async () => {
  session.mockResolvedValue(true);
  apiClient.defaults.adapter = async (config) => {
    throw { config, response: { status: 401 } };
  };
  mount();
  await screen.findByText("Protected queues");
  await act(async () => {
    await apiClient.get("/api/redis_info").catch(() => {});
  });
  expect(
    await screen.findByRole("button", { name: "Sign in" })
  ).toBeInTheDocument();
  expect(screen.queryByText("Protected queues")).not.toBeInTheDocument();
});

test("preserves deep link across authentication", async () => {
  window.history.replaceState({}, "", "/settings");
  mount();
  await signIn();
  expect(
    await screen.findByRole("button", { name: "Sign out" })
  ).toBeInTheDocument();
  expect(window.location.pathname).toBe("/settings");
});

test("keeps existing unauthenticated library usage working", async () => {
  window.AUTH_ENABLED = false;
  mount();
  expect(await screen.findByText("Protected queues")).toBeInTheDocument();
  expect(session).not.toHaveBeenCalled();
  expect(
    screen.queryByRole("button", { name: "Sign out" })
  ).not.toBeInTheDocument();
});

test("clears private state on expiry and ignores late completions", async () => {
  session.mockResolvedValue(true);
  mount();
  await screen.findByText("Protected queues");
  act(() => {
    window.dispatchEvent(new Event(SESSION_ENDED));
  });
  store.dispatch({ type: "LIST_QUEUES_ERROR", error: "late private details" });
  expect(
    await screen.findByRole("button", { name: "Sign in" })
  ).toBeInTheDocument();
  expect(JSON.stringify(store.getState())).not.toContain(
    "late private details"
  );
});
