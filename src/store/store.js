// src/store/store.js
import { configureStore } from "@reduxjs/toolkit";
import trackerReducer from "@/feature/tracker/trackerSlice";

export const store = configureStore({
  reducer: {
    tracker: trackerReducer,
  },
});