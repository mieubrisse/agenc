- [ ] Add Claude Code instructions for installing agenc (CLAUDE.md or skill that explains how to `go install` or clone + build)
- [ ] Create an MCP server that allows AgenC agents to clone repos into their workspace directory
- [ ] Roll my agenc-config global CLAUDE settings into agenc defaults
- [ ] Fix missing logging + race condition in template change reload path
    - The `configChanged` handler in `wrapper.go:183-198` has zero logging (unlike the `globalConfigChanged` handler which logs at line 204). Template rsyncs and state transitions happen silently.
    - Race condition: when Claude exits naturally, both `claudeStateIdle` (from the Stop hook) and `claudeExited` arrive on the select simultaneously. If `claudeExited` wins while state is `StateRestartPending` (not yet `StateRestarting`), the wrapper exits at line 181 without restarting — the pending restart is lost.
- [ ] Reload when non-CLAUDE.md/settings.json changes happen (skills, subagents, etc.)
- [ ] Add Bash aliases so you can `cd` to an agent's workdir
- [ ] Add crons with scheduled work tracker (dump work into the queue, cron picks it up)
- [ ] OPTIMIZATION: Daemon has fsnotify on `config.yml` and refreshes its understanding of the config only once when it's updated
    - Prevents reading the config on every loop
- [ ] Maybe some way to save information for later???? E.g. I'm doing research in a research tab, and I want to come back to it later
- [ ] Check on perf of the daemon constantly running a bunch of "is this PID alive?" checks
- [ ] Find or build an inter-agent chat system
- [ ] Create frontend app
- [ ] Add observability and mission metrics
- [ ] Integrate beads
- [ ] Agent-switching mid-conversation
- [ ] Claude command to fire off edits to agent templates

### Docker
- [ ] Figure out how to run missions in Docker


### Tmux fanciness
- [ ] When calling a new mission, open one half Claude and the other half the working directory of the thing
- [ ] Slash command for quickly editing agent config
    - Maybe need to clone the existing mission???
    - DEFINITELY need to copy the Claude history, so that the new editing thing gets it
- [ ] Add tmux fanciness so that when opening a new pane from an existing agent session, it opens it in the MISSION_DIRPATH
- [ ] Add tmux fanciness: a popup window that allows jumping to anything (new repo, new agent, open a Git worktree on an existing project, whatever)
- [ ] Add some way to mark a window/pane as unread, so you know to come back to it



-----


OLD TODOS TXT
=============



META:
- maybe encoding in the AIs the Type 1/2 rules: act slowly and deliberately for actions that will take a long time to repair, and very fast for things that can be quickly undone
- for making this user-friendly, I think I probably need to allow easy association of an MCP server with an agent, and I can probably use Tadas' MCP sub-registry that's only the vetted, secure ones
- perhaps, give every prompt a thing where it asks clarifying questions first to understand what exactly the work even is?
    - OR, maybe there's an "agent dispatcher" that an agent phones when it wants to "phone-a-friend"? and the agent dispatcher forces the calling agent to get really clear on why it's calling, and the work it wants from the other agent?


Framework
=========
- Need a way to include shared snippets of prompt
- Entire thing should be powered by a Git repository
- Need to have a way to multiplex between the different agents doing things - check in on them, etc.
- Need to have a shared space for the agents to put work
- Proooooobably need a Fractal Outcomes-like thing
    - WITH a way to attach documentation



Agents To Create
================

### taichii ono (OR maybe this is the prompt engineer???)
- runs tests on the various prompts, and sees how well they followed instructions
- tries to break prompts
    - sort of like chaos monkey, for prompts???
- if it breaks them, goes back to the prompt engineer and tries to fix

### doctor/nutritionist
- probably, the only one who manages medical history
    - both writes it and reads it
- Can also provide this to other agents as needed

### nutritionist
- sees what Bete cooks for me each week, what I'm eating
- communicates to Bete
- sees my blood tests
- sees what Dr Patrick wants me eating

### todoist assistant
- clearing inbox
- managing book notes
- periodic cleaning: if projects get archived, then archiving their project support directory too

### Hevy workout builder
- takes in Mi's custom format
- loads it into Hevy

### coach/trainer
- sees Hevy
- sees Fitbit
- understands my medical history
- can do reflection on my past week (seeing notes)
- look at how my RPE changes over the course of the sets. does it rise? drop? this is a good indicator of how much my body can support the load

### financial planner/advisor
- reminds me about tax time (PERHAPS, even starts to throw tasks into my Todoist?)
    - MAYBE even starts to throw tasks into my calendar...????
- probably sees my YNAB
- Maybe sees my ProjectionLab???
    - Could potentially update my asset lists!!!

### calender manager
- knows my scheduling preferences
- serves as gatekeeper for anyone/thing that wants to put time on my calendar

### substack writer
- handles writing
- encodes voice
- idea brainstorming & planning

### substack publisher
- handles the nitty-gritty of publishing to Substack
- setting the right image
- knows how to do the "TODO SUBSCRIBE BUTTON" swapping
- leaves it as a draft, with a notification

### flight finder
- oh please god

### voice finder
- asks various questions to understand the user and how they write
- has AI write stuff, and then has the user correct it into their voice
    - uses the diff to find the user's voice

### web prompt finder + improver
- search the web for prompts relevant to my agents
- compare the prompt, and 

### whatsapp communicator
- keeps context on how I talk to each person
- fits specific API
- can plug into the Personal CRM for communicating with people

### service finder
- Maybe... a Service Finder??? finding a plumber, 
    - e.g. "find me a dentist here in Sao Paulo near where I live"
    - e.g. "find me a well-rated urologist

### restaurant researcher
- encodes my preferences for cafes
- encodes my preferences for restaurants

### before starting a book researcher
(from James Clear's "how to read a book")
- what's the context behind the book?
- when was it written?
- WHY was it written?

### mental health coach
- when I'm feeling stressed or unhappy or angry, helps me first focus on Doing Nothing
    - return-to-positive
- avoids negative leverage by ensuring I don't say or do anything dumb
- helps me analyze what happened, and extract lessons out of it
    - honestly, this probably isn't even THAT hard... just do the reflection prompt

### journal analyst
- reads my journal and checks on my mental state, to see how I'm doing
- can report data to my trainer, to my return-to-positive therapist, to doctor (really to anyone)
- perhaps, synthesizes this into a "state of Kevin" that gets published?
- extracting useful & interesting content ideas out for later transformation into Instagram content

### day effectiveness/vibe analyzer
- maybe I can use my ActivityWatch information to see how effective I was - how much time I spent on entertainment and nonsense
    - perhaps, could also see how much time I spent context-switching, which would maybe indicate how distracted I was (?)
- check my heartrate too, to see how stressed I was
- check the music that I listened to, which is a pretty good indicator of my mind state!

### git synchronization bot
- Finding unpushed changes

### alignment agent
- responsible for figuring out how the human thinks
- can use various tests
    - we want mind readers! which means, we need to understand the mind
    - EX: AI writes something, you correct it, then the alignment agent extracts the differences out into a "your voice" script
- maybe OCEAN is predictive of how humans think?
- **You need CONSTANT alignment, so must be checking in periodically to stay re-aligned!**
    - Humans change over days and weeks

### Grain meeting analyzer
- uses Grain MCP server
