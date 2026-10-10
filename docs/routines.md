# Routines

A routine is a task a bot runs on a schedule in its DM, after `../lorca/docs/architecture/routines.md`: a morning brief, an hourly check, a weekly report. The bot owns its routines: the user asks in chat and the bot sets one up with the `routines` tool, and edits, pauses, resumes, runs or deletes it the same way. Every turn's system prompt explains routines, names this computer's timezone, and lists the bot's own with their schedules in words, their next runs and how the last went.

## Schedules (`schedule`)

`schedule.Parse(spec, tz)` reads `every 30m`, `every 2h`, `every 1d`, `every 1w`, or five cron fields (`0 9 * * 1-5`: lists, ranges, steps, month and day names, 7 as Sunday, a day of the month or of the week when both are given) in an IANA timezone, `""` being the computer's (`LocalZone`: `TZ`, else `/etc/localtime`'s zone). Nothing may run more often than every five minutes. `Next(t)` is the first run after `t`: an interval counts from the latest run (or the creation or resume); cron finds the next matching minute in the routine's zone, and a minute daylight saving skips does not fire that day. The IANA rules are embedded (`time/tzdata`), so Windows reads them too. `Text` says it in words: "Every 2 hours", "Weekdays at 9:00", "Every day at 8:30", "Every Monday at 9:00", "On the 1st of every month at 8:30", else "Cron …".

## Store

`store.Routine`: bot, name, prompt (an instruction to the bot itself), schedule, timezone, missed-run policy (`coalesce`, the default, or `skip`), check (JavaScript, see Checks), enabled and why it paused, the due time last taken, the last run and its outcome (`sent`, `pass`, `error`), the last check and how it went (`quiet`, `ready`, `failed`). Routines go with their bot.

## Running them (`team`)

`Team.StartRoutines(ctx)` looks every 30 seconds (`TickRoutines`). A routine is due when the next run after the latest of its last due time, its resume and its creation has come; one whose DM is at work waits for the next tick. The due time is saved before any work, so a restart neither runs it again nor drops it. Missed due times (the app was closed) make one run with `coalesce`; with `skip`, a due time more than a minute late is taken without a run.

When the user has not written in any chat for seven days (`AwayAfter`), due routines pause instead (`paused_reason: away`) and the DM gets a notice (`KindNotice`, which bots do not read).

A run is a message from `store.System` in the bot's DM, with the routine in its `Meta`. The bot reads it as `[Routine "Name" is running on its schedule; nobody is typing. …] Task: …` and, once it has answered, as `[Routine "Name" ran on its schedule.] Task: …`. A reply that `IsPass` is taken out, leaving the marker; the outcome is kept on the routine. `RunRoutine` runs one now; asked for during a turn in that DM, it runs after it.

**Checks.** With `Team.Checks` set (the desktop sets `Team.RunCheck`, see `plugins.md`), a routine with a `check` runs it first at each due time: nothing returned is a quiet check and no turn; what it returns starts the run, read after the task as data from its tools and not instructions; a failing script starts the run with its error so the bot fixes it.

## The tool

`routines { action: list | create | edit | pause | resume | run | delete, routine?, name?, prompt?, schedule?, timezone?, missed_run_policy?, check? }`: a routine by id or name; a schedule that does not read is an error the bot sees; a new schedule or timezone, and a resume, count from now.

## The desktop

A bot's inspector has a Routines section: a row per routine with its schedule in words and next run (or Paused, Paused while you were away) and a switch; with none, it says to ask the bot. A row opens a sheet: the schedule and timezone, missed runs, the last run and check, the task and the check's code, and Delete, Edit in Chat (which puts `Edit my routine "Name": ` in the DM's composer), Pause/Resume and Run Now. A run shows in the transcript as "Routine · Name". Out of sight, its reply (or failure) posts a system notification titled "Bot · Routine"; a pass posts none; a pause for being away posts one too; and the Dock's badge counts what waits unread.

Checked against DeepSeek: asked in plain words for a weekday 8:30 reminder in Shanghai time, Chef created `30 8 * * 1-5` in `Asia/Shanghai`, and a run sent the one-line reminder.
