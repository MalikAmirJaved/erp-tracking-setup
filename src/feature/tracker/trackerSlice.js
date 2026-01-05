// src/feature/tracker/trackerSlice.js
import { createSlice, createAsyncThunk } from "@reduxjs/toolkit";

const API_URL = import.meta.env.VITE_REACT_APP_TRACKING_URL;

export const checkScreenshotPermission = createAsyncThunk(
  "tracker/checkPermission",
  async (_, { rejectWithValue }) => {
    try {
      const res = await fetch(`${API_URL}/start`, { method: "POST" });
      if (res.ok && (await res.text()) === "success") {
        return true;
      }
      return false;
    } catch {
      return false;
    }
  }
);

export const startTracking = createAsyncThunk(
  "tracker/startTracking",
  async (_, { rejectWithValue }) => {
    try {
      const res = await fetch(`${API_URL}/start`, { method: "POST" });
      if (!res.ok) throw new Error("Failed");
      if ((await res.text()) !== "success") throw new Error("Invalid response");
      return true;
    } catch {
      return rejectWithValue("failed");
    }
  }
);

export const stopTracking = createAsyncThunk(
  "tracker/stopTracking",
  async (_, { rejectWithValue }) => {
    try {
      await fetch(`${API_URL}/stop`, { method: "POST" });
      return true;
    } catch {
      return rejectWithValue("failed");
    }
  }
);

const trackerSlice = createSlice({
  name: "tracker",
  initialState: {
    status: "inactive", // inactive, active, break
    time: 0,
    startTime: null,
    loading: false,
    permissionGranted: null,
  },
  reducers: {
    setBreakMode: (state) => {
      state.status = state.status === "break" ? "active" : "break";
    },
    resetTracker: (state) => {
      state.status = "inactive";
      state.time = 0;
      state.startTime = null;
    },
  },
  extraReducers: (builder) => {
    builder
      .addCase(checkScreenshotPermission.pending, (state) => {
        state.permissionGranted = null;
      })
      .addCase(checkScreenshotPermission.fulfilled, (state, action) => {
        state.permissionGranted = action.payload;
      })
      .addCase(startTracking.pending, (state) => {
        state.loading = true;
      })
      .addCase(startTracking.fulfilled, (state) => {
        state.loading = false;
        state.status = "active";
        state.startTime = new Date().toISOString();
        state.time = 0;

      })
      .addCase(stopTracking.pending, (state) => {
        state.loading = true;
      })
      .addCase(stopTracking.fulfilled, (state) => {
        state.loading = false;
        state.status = "inactive";
        state.time = 0;
        state.startTime = null;

      })
      .addCase(stopTracking.rejected, (state) => {
        state.loading = false;
        state.status = "inactive";
        state.time = 0;
        state.startTime = null;

      });
  },
});

export const { setBreakMode, resetTracker } = trackerSlice.actions;
export default trackerSlice.reducer;