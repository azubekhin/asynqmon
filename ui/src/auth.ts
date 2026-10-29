import { authClient } from "./httpClient";

export async function session(): Promise<boolean> {
  const response = await authClient.get(`${window.ROOT_PATH}/api/auth/session`);
  return response.data.authenticated === true;
}

export async function login(username: string, password: string): Promise<void> {
  await authClient.post(`${window.ROOT_PATH}/api/auth/login`, {
    username,
    password,
  });
}

export async function logout(): Promise<void> {
  await authClient.post(`${window.ROOT_PATH}/api/auth/logout`);
}
