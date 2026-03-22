package templates

const SystemdUnit = `[Unit]
Description=Specter Agent
After=network.target

[Service]
Type=simple
User=specter
Group=specter
WorkingDirectory=/home/specter/app
ExecStart=/usr/local/bin/bun run start
Restart=always
RestartSec=10
StartLimitBurst=5
StartLimitIntervalSec=300
EnvironmentFile=/home/specter/app/.env
StandardOutput=journal
StandardError=journal
SyslogIdentifier=specter-agent

ProtectSystem=strict
ReadWritePaths=/home/specter/app/data /home/specter/app/logs /home/specter/app/.sessions
MemoryMax=2G

[Install]
WantedBy=multi-user.target
`
