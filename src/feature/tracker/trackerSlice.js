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

export const checkTrackingStatus = createAsyncThunk(
  "tracker/checkStatus",
  async (_, { rejectWithValue }) => {
    try {
      const res = await fetch(`${API_URL}/status`, { method: "GET" });
      if (res.ok) {
        const text = await res.text();
        return text === "running";
      }
      return false;
    } catch {
      return false;
    }
  }
);

// Add a break tracking thunk to stop/start the Go tracker
export const toggleBreakTracking = createAsyncThunk(
  "tracker/toggleBreak",
  async (isBreak, { rejectWithValue }) => {
    try {
      if (isBreak) {
        // Stop tracking during break
        const res = await fetch(`${API_URL}/stop`, { method: "POST" });
        if (!res.ok) throw new Error("Failed to stop");
        return "break";
      } else {
        // Resume tracking
        const res = await fetch(`${API_URL}/start`, { method: "POST" });
        if (!res.ok) throw new Error("Failed to start");
        return "active";
      }
    } catch (error) {
      return rejectWithValue(error.message);
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
    isAutoStarted: false,
  },
  reducers: {
    setBreakMode: (state) => {
      state.status = state.status === "break" ? "active" : "break";
    },
    resetTracker: (state) => {
      state.status = "inactive";
      state.time = 0;
      state.startTime = null;
      state.isAutoStarted = false;
    },
    // Add a new reducer for auto-start
    setAutoStarted: (state) => {
      state.status = "active";
      state.startTime = new Date().toISOString();
      state.time = 0;
      state.isAutoStarted = true;
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

      })
      .addCase(toggleBreakTracking.pending, (state) => {
        state.loading = true;
      })
      .addCase(toggleBreakTracking.fulfilled, (state, action) => {
        state.loading = false;
        state.status = action.payload;
        // Don't reset timer when breaking/resuming
      })
      .addCase(toggleBreakTracking.rejected, (state) => {
        state.loading = false;
        // Keep current status on error
      })
      .addCase(checkTrackingStatus.fulfilled, (state, action) => {
        if (action.payload && state.status === "inactive") {
          state.status = "active";
          state.startTime = new Date().toISOString();
          state.isAutoStarted = true;
        }
      });
  },
});

export const { setBreakMode, resetTracker, setAutoStarted } = trackerSlice.actions;
export default trackerSlice.reducer;