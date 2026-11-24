package main

// taskName is the scheduled task that kept the tunnel in step with the network up to 1.0.20. Windows holds a
// SYSTEM task's triggers back while energy saver is on (see watch_windows.go); the watcher service does its work
// now, and an upgrade deletes the task.
const taskName = "Hotspot Auto Connect"
