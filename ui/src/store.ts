import {
  Reducer,
  AnyAction,
  combineReducers,
  configureStore,
} from "@reduxjs/toolkit";
import settingsReducer from "./reducers/settingsReducer";
import queuesReducer from "./reducers/queuesReducer";
import tasksReducer from "./reducers/tasksReducer";
import groupsReducer from "./reducers/groupsReducer";
import serversReducer from "./reducers/serversReducer";
import schedulerEntriesReducer from "./reducers/schedulerEntriesReducer";
import snackbarReducer from "./reducers/snackbarReducer";
import queueStatsReducer from "./reducers/queueStatsReducer";
import redisInfoReducer from "./reducers/redisInfoReducer";
import metricsReducer from "./reducers/metricsReducer";
import { loadState } from "./localStorage";

const combinedReducer = combineReducers({
  settings: settingsReducer,
  queues: queuesReducer,
  tasks: tasksReducer,
  groups: groupsReducer,
  servers: serversReducer,
  schedulerEntries: schedulerEntriesReducer,
  snackbar: snackbarReducer,
  queueStats: queueStatsReducer,
  redis: redisInfoReducer,
  metrics: metricsReducer,
});

// Redux also dispatches initialization and auth lifecycle actions, which each
// service reducer handles through its default branch.
export type AppState = ReturnType<typeof combinedReducer>;
const rootReducer = combinedReducer as Reducer<AppState, AnyAction>;

let sessionEnded = false;
function sessionReducer(
  state: AppState | undefined,
  action: AnyAction
): AppState {
  if (action.type === "AUTH_SESSION_ENDED") {
    sessionEnded = true;
    return rootReducer({ settings: state?.settings } as AppState, {
      type: "@@INIT",
    });
  }
  if (action.type === "AUTH_SESSION_STARTED") sessionEnded = false;
  // Discard late completions from requests belonging to an expired session.
  if (sessionEnded && !action.type.startsWith("@@"))
    return state || rootReducer(undefined, action);
  return rootReducer(state, action);
}

const preloadedState = loadState();

const store = configureStore({
  reducer: sessionReducer,
  preloadedState,
});

export default store;
