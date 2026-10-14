import type { Icon } from "@/content/landings";

// Visual choices, separate from editorial content.
export const editorialDesigns: Record<string, { icon: Icon; steps: Icon[]; capabilities: Icon[] }> = {
  "fully-managed-cloud": {
    "icon": "cloud",
    "steps": [
      "rocket",
      "shield",
      "archive"
    ],
    "capabilities": [
      "cloud",
      "shield",
      "archive"
    ]
  },
  "apps-and-databases": {
    "icon": "database",
    "steps": [
      "layout",
      "database",
      "cpu"
    ],
    "capabilities": [
      "layout",
      "cpu",
      "database",
      "layers",
      "hard-drive",
      "gauge"
    ]
  },
  "app-access": {
    "icon": "lock",
    "steps": [
      "key",
      "layout",
      "globe"
    ],
    "capabilities": [
      "globe",
      "key",
      "users",
      "layout",
      "eye"
    ]
  },
  "automatic-sleep": {
    "icon": "moon",
    "steps": [
      "moon",
      "activity",
      "database"
    ],
    "capabilities": [
      "layout",
      "database",
      "wrench",
      "hard-drive"
    ]
  },
  "project-management": {
    "icon": "activity",
    "steps": [
      "scroll",
      "rocket",
      "history"
    ],
    "capabilities": [
      "scroll",
      "gauge",
      "history",
      "activity",
      "layers",
      "bot"
    ]
  },
  "ai-community": {
    "icon": "sparkles",
    "steps": [
      "bot",
      "link",
      "refresh"
    ],
    "capabilities": [
      "link",
      "database",
      "history"
    ]
  },
  "vibe-coders": {
    "icon": "pencil",
    "steps": [
      "rocket",
      "link",
      "refresh"
    ],
    "capabilities": [
      "globe",
      "database",
      "history"
    ]
  },
  "developers": {
    "icon": "code",
    "steps": [
      "rocket",
      "database",
      "history"
    ],
    "capabilities": [
      "cpu",
      "scroll",
      "terminal"
    ]
  },
  "businesses": {
    "icon": "building",
    "steps": [
      "rocket",
      "key",
      "refresh"
    ],
    "capabilities": [
      "users",
      "key",
      "coins",
      "handshake",
      "building",
      "activity"
    ]
  },
  "implementation-partners": {
    "icon": "handshake",
    "steps": [
      "handshake",
      "wrench",
      "tag"
    ],
    "capabilities": [
      "rocket",
      "database",
      "users"
    ]
  },
  "apps-built-with-ai": {
    "icon": "bot",
    "steps": [
      "bot",
      "link",
      "refresh"
    ],
    "capabilities": [
      "globe",
      "history",
      "moon"
    ]
  },
  "websites": {
    "icon": "globe",
    "steps": [
      "rocket",
      "globe",
      "pencil"
    ],
    "capabilities": [
      "file",
      "globe",
      "refresh"
    ]
  },
  "web-apps": {
    "icon": "server",
    "steps": [
      "rocket",
      "database",
      "history"
    ],
    "capabilities": [
      "server",
      "cpu",
      "database",
      "layers",
      "hard-drive",
      "scroll"
    ]
  },
  "ai-apps-and-agents": {
    "icon": "cpu",
    "steps": [
      "rocket",
      "key",
      "scroll"
    ],
    "capabilities": [
      "cpu",
      "key",
      "database",
      "activity"
    ]
  },
  "internal-apps": {
    "icon": "layout",
    "steps": [
      "key",
      "layout",
      "refresh"
    ],
    "capabilities": [
      "lock",
      "users",
      "key",
      "layout"
    ]
  },
  "client-projects": {
    "icon": "briefcase",
    "steps": [
      "rocket",
      "refresh",
      "handshake"
    ],
    "capabilities": [
      "rocket",
      "users",
      "scroll"
    ]
  },
  "white-label-infrastructure": {
    "icon": "tag",
    "steps": [
      "layers",
      "coins",
      "cloud"
    ],
    "capabilities": [
      "tag",
      "file",
      "cloud"
    ]
  },
  "easy-deployment": {
    "icon": "rocket",
    "steps": [
      "rocket",
      "globe",
      "refresh"
    ],
    "capabilities": [
      "code",
      "package",
      "history",
      "terminal",
      "globe",
      "bot"
    ]
  }
};
