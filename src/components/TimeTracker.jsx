// src/components/TimeTracker.jsx
import { Play, Square, Coffee, ArrowDownToLine, Minus } from "lucide-react";
import { useSelector, useDispatch } from "react-redux";
import {
  startTracking,
  stopTracking,
  setBreakMode,
} from "@/feature/tracker/trackerSlice";
import { useEffect, useState } from "react";

const TimeTracker = () => {
  const dispatch = useDispatch();
  const { status, time, startTime, loading } = useSelector(
    (state) => state.tracker
  );
  const { user } = useSelector((state) => state.auth);

  // This forces the timer to update every second
  const [, setTick] = useState(0);
  useEffect(() => {
    if (status !== "active" || !startTime) return;
    const interval = setInterval(() => setTick((v) => v + 1), 1000);
    return () => clearInterval(interval);
  }, [status, startTime]);

  const formatTime = (seconds) => {
    const h = Math.floor(seconds / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    const s = seconds % 60;
    return `${h.toString().padStart(2, "0")}:${m
      .toString()
      .padStart(2, "0")}:${s.toString().padStart(2, "0")}`;
  };

  const displayTime = (() => {
    if (status !== "active" || !startTime) return formatTime(time);
    const elapsed = Math.floor((Date.now() - new Date(startTime)) / 1000);
    return formatTime(time + elapsed);
  })();

  const getStatusText = () => {
    switch (status) {
      case "active":
        return "Check In";
      case "break":
        return "On Break";
      default:
        return "Check Out";
    }
  };

  const handleMinimize = () => window.electronAPI?.hideWindow?.();

  return (
    <div className="h-screen w-screen bg-white flex flex-col select-none overflow-hidden ">

      {/* Draggable orange title bar */}
      <div
        style={{ WebkitAppRegion: "drag" }}
        className="bg-orange-600 flex items-center justify-end px-4 py-1 shrink-0 -webkit-app-region-drag text-white"
      >
        <button
          onClick={handleMinimize}
          className="rounded-full hover:bg-orange-700 transition-colors -webkit-app-region-no-drag"
          title="Minimize to tray"
        >
          <Minus className="w-5 h-5" />
        </button>
      </div>

      {/* Main content */}
      <div className="flex flex-col justify-between h-full py-3">
        <div>
          <h1 className="font-semibold text-foreground w-full truncate ml-4">
            Good morning,{" "}
            <span className="text-blue-700">{user?.name || "Unknown"} </span>👋
          </h1>
          <div className="">
            <div className=" flex items-center gap-4 justify-between mx-5 text-xl">
              <span className="  font-bold text-orange-700 ">
                {getStatusText()}
              </span>
              <span className="  font-bold text-orange-700 tabular-nums">
                {displayTime}
              </span>
            </div>
          {startTime && status !== "inactive" && (
            <div className="px-5 ">
              <p className="w-full justify-end items-center gap-2 flex  ">
                Started{" "}
                {new Date(startTime).toLocaleTimeString([], {
                  hour: "2-digit",
                  minute: "2-digit",
                })}
              </p>
            </div>
          )}
          </div>
        </div>

        {/* Buttons – always visible, bigger and stronger */}
        <div className=" w-full ">
          {status === "inactive" ? (
            <div className="mx-4">
              <button
                onClick={() => dispatch(startTracking())}
                disabled={loading}
                className=" w-full justify-center items-center gap-2 flex py-2 px-10 bg-orange-500 hover:bg-orange-600 disabled:opacity-70 text-white  font-bold rounded-2xl shadow-lg transition-all"
              >
                <Play className="w-10 h-10" />
                Start
              </button>
            </div>
          ) : (
            <div className="flex justify-between mx-4  gap-4">
              <button
                onClick={() => dispatch(setBreakMode())}
                className={`flex items-center gap-4 py-1.5 w-full justify-center rounded-2xl font-bold  shadow-lg transition-all ${
                  status === "break"
                    ? "bg-orange-500 text-white"
                    : "bg-gray-200 text-gray-800 hover:bg-gray-300"
                }`}
              >
                <Coffee className="w-6 h-6" />
                {status === "break" ? "Resume" : "Break"}
              </button>

              <button
                onClick={() => dispatch(stopTracking())}
                disabled={loading}
                className="flex items-center gap-4 py-1.5 w-full justify-center bg-red-600 hover:bg-red-700 disabled:opacity-70 text-white  font-bold rounded-2xl shadow-lg transition-all"
              >
                <Square className="w-6 h-6" />
                End
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

export default TimeTracker;
