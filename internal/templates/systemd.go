package templates

const SystemdUnit = `[Unit]
Description=Specter Agent
After=network.target
StartLimitBurst=5
StartLimitIntervalSec=300

[Service]
Type=simple
User=specter
Group=specter
WorkingDirectory=/home/specter/app
ExecStart=/usr/local/bin/bun run start
Restart=always
RestartSec=10
EnvironmentFile=/home/specter/app/.env
StandardOutput=journal
StandardError=journal
SyslogIdentifier=specter-agent

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=/home/specter/app
PrivateTmp=true
MemoryMax=2G
MemoryHigh=1536M
TasksMax=256

[Install]
WantedBy=multi-user.target
`
