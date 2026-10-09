package config

// ExampleYAML is written by "bharbor init". It is a complete, commented
// configuration whose single example target is disabled until the user edits it.
const ExampleYAML = `# BackupHarbor configuration
# Relative paths below are resolved against the directory that contains this file.
# Passwords are never stored here; see the "password" block of each target.
version: 1

language: auto        # auto | en | tr
theme: system         # system | light | dark

restic:
  path: ""            # full path to restic.exe; empty = the restic.exe shipped with BackupHarbor, then PATH

temp_dir: ""          # scratch space for restores; empty = %LOCALAPPDATA%\BackupHarbor\work
concurrency: 1        # targets verified in parallel (1-8)
timeout: 2h           # per-target time limit (1m - 48h)
log_level: info       # debug | info | warn | error

reports:
  dir: ""             # empty = %LOCALAPPDATA%\BackupHarbor\reports
  formats: []         # extra files written after each run: html, txt (JSON is always kept)

schedule:             # used by "bharbor schedule install"
  frequency: daily    # daily | weekly
  time: "03:00"       # 24-hour HH:MM
  day: ""             # weekly only: monday ... sunday

targets:
  - name: example-backup
    enabled: false                  # set to true after editing this target
    repository: 'D:\Backups\restic' # local path or any restic repository URL
    password:
      source: credential            # credential | env | file
      # credential: example-backup  # Credential Manager name (default: target name)
      # env: RESTIC_PASSWORD_EXAMPLE
      # file: 'C:\Secure\restic.pw'
    snapshot:
      select: latest                # latest | tag | id
      # tags: [daily]               # for select: tag
      # id: 1a2b3c4d                # for select: id
      # host: MYPC                  # optional host filter
    check:
      enabled: true
      read_data: none               # none | subset | all (all reads every pack: slow, full proof)
      # subset: 5%                  # for read_data: subset (n/t, N%, or a size such as 500M)
    restore:
      enabled: true
      verify: true                  # re-read restored files and compare with repository hashes
      min_files: 1
      # include: ['/D/Documents']   # restore only these paths (restic --include)
    rules:                          # content checks on the restored files
      # - name: databases
      #   path: '**/*.sqlite'
      #   type: sqlite              # exists non_empty text json xml zip pdf sqlite sha256
      #   required: true
`
