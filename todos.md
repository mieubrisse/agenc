- [ ] Edit agenc in agenc
    - [ ] Create a software developer agent template
        - [ ] Create a way to make new agents
- [ ] Move agent-templates into config.yml inside `config` directory
- [ ] Add Bash aliases so you can `cd` to an agent's workdir
- [ ] Reload when global Claude config reloads
- [ ] Add crons with scheduled work tracker (dump work into the queue, cron picks it up)
- [ ] Add option for agenc-managed config
- [ ] Daemon has fsnotify on `config.yml` and refreshes its understanding of the config whenever it's updated
- [ ] Add the ability for agents to request other repo copies

### Docker
- [ ] Figure out how to run missions in Docker


### Tmux fanciness
- [ ] When calling a new mission, open one half Claude and the other half the working directory of the thing
- [ ] Add tmux fanciness so that when opening a new pane from an existing agent session, it opens it in the MISSION_DIRPATH
- [ ] Add tmux fanciness: a popup window that allows jumping to anything (new repo, new agent, open a Git worktree on an existing project, whatever)
