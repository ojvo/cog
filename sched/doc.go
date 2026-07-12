// Package sched provides scheduling and time utilities:
//   - CronScheduler: cron-based job scheduler with dynamic register/unregister,
//     health check, and panic recovery
//   - CronParser: 5/6-field cron expression parser with descriptors (@daily, etc.)
//   - Time helpers: format/parse, date arithmetic, weekday, timestamp conversion
package sched
