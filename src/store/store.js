// src/store/store.js
import { configureStore } from "@reduxjs/toolkit";
import authReducer from "@/feature/auth/authSlice";
import trackerReducer from "@/feature/tracker/trackerSlice";

export const store = configureStore({
  reducer: {
    auth: authReducer,
    tracker: trackerReducer,
  },
});