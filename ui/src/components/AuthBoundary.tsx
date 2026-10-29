import { AxiosError } from "axios";
import React, { useCallback, useEffect, useState } from "react";
import { useDispatch } from "react-redux";
import {
  Box,
  Button,
  CircularProgress,
  Paper,
  TextField,
  Typography,
} from "@material-ui/core";
import { makeStyles } from "@material-ui/core/styles";
import { session, login, logout } from "../auth";
import { invalidateRequests, SESSION_ENDED } from "../httpClient";
import { ReactComponent as Logo } from "../images/logo-color.svg";

const useStyles = makeStyles((theme) => ({
  screen: {
    minHeight: "100vh",
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    padding: theme.spacing(3),
    background: theme.palette.background.default,
  },
  card: { width: "100%", maxWidth: 380, padding: theme.spacing(4) },
  logout: {
    position: "fixed",
    top: theme.spacing(2),
    right: theme.spacing(3),
    zIndex: theme.zIndex.appBar + 1,
  },
}));

export default function AuthBoundary({
  children,
}: {
  children: React.ReactNode;
}) {
  const classes = useStyles();
  const dispatch = useDispatch();
  const [status, setStatus] = useState<
    "checking" | "anonymous" | "authenticated"
  >(window.AUTH_ENABLED ? "checking" : "authenticated");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const endSession = useCallback(() => {
    invalidateRequests();
    dispatch({ type: "AUTH_SESSION_ENDED" });
    setStatus("anonymous");
    setPassword("");
    setError("");
  }, [dispatch]);

  useEffect(() => {
    if (!window.AUTH_ENABLED) return;
    let active = true;
    window.addEventListener(SESSION_ENDED, endSession);
    session()
      .then((authenticated) => {
        if (active) setStatus(authenticated ? "authenticated" : "anonymous");
      })
      .catch(() => {
        if (active) {
          setStatus("anonymous");
          setError("Could not check your session. Please try signing in.");
        }
      });
    return () => {
      active = false;
      window.removeEventListener(SESSION_ENDED, endSession);
    };
  }, [endSession]);

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      await login(username, password);
      dispatch({ type: "AUTH_SESSION_STARTED" });
      setPassword("");
      setStatus("authenticated");
    } catch (err) {
      const code = (err as AxiosError).response?.status;
      setError(
        code === 401
          ? "Invalid username or password"
          : code === 429
          ? "Too many login attempts. Try again in a minute."
          : "Could not sign in. Please try again."
      );
      setPassword("");
    } finally {
      setBusy(false);
    }
  }

  async function signOut() {
    setBusy(true);
    setError("");
    try {
      await logout();
      endSession();
    } catch {
      setError("Could not sign out. Please try again.");
    } finally {
      setBusy(false);
    }
  }

  if (status === "checking")
    return (
      <div className={classes.screen}>
        <CircularProgress aria-label="Checking session" />
      </div>
    );
  if (status === "authenticated")
    return (
      <>
        {children}
        {window.AUTH_ENABLED && (
          <Box className={classes.logout}>
            <Button variant="outlined" onClick={signOut} disabled={busy}>
              Sign out
            </Button>
            {error && (
              <Typography role="alert" color="error">
                {error}
              </Typography>
            )}
          </Box>
        )}
      </>
    );
  return (
    <div className={classes.screen}>
      <Paper className={classes.card} variant="outlined">
        <Box mb={3}>
          <Logo width="100%" height={48} />
        </Box>
        <Typography variant="h5" component="h1">
          Sign in to Asynqmon
        </Typography>
        <form onSubmit={submit}>
          <TextField
            id="auth-username"
            label="Username"
            autoComplete="username"
            autoFocus
            fullWidth
            required
            margin="normal"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            disabled={busy}
          />
          <TextField
            id="auth-password"
            label="Password"
            type="password"
            autoComplete="current-password"
            fullWidth
            required
            margin="normal"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            disabled={busy}
          />
          {error && (
            <Typography role="alert" color="error">
              {error}
            </Typography>
          )}
          <Box mt={3}>
            <Button
              type="submit"
              color="primary"
              variant="contained"
              fullWidth
              disabled={busy}
            >
              {busy ? "Signing in…" : "Sign in"}
            </Button>
          </Box>
        </form>
      </Paper>
    </div>
  );
}
