import { useEffect, useState } from "react";
import { CallOverlay } from "./call/CallOverlay";
import { CallProvider } from "./call/CallContext";
import type { NotificationEntry } from "./services/appServices";
import { MediaProvider } from "./ui/media/MediaContext";
import { Avatar, CallHistory, ChannelScreen, Channels, ChatList, CollabScreen, Communities, CommunityScreen, Contacts, CreateGroup, DiscoverScreen, GroupInfoScreen, Login, NewChat, Profile, Search, Settings, Status, Thread, Verify, WhiteboardScreen } from "./ui/screens";
import { Icon, type IconName } from "./ui/icons";
import { ServicesProvider, useServices } from "./ui/ServicesContext";

/** NotificationToasts shows a transient banner for each in-app notification
 *  (a message in a chat you weren't viewing, honouring mute). Tap to open it. */
function NotificationToasts({ onOpen }: { onOpen: (conversationId: string) => void }) {
  const { services } = useServices();
  const [toasts, setToasts] = useState<NotificationEntry[]>([]);
  useEffect(() => {
    return services.onToast((n) => {
      setToasts((cur) => [...cur, n].slice(-3));
      setTimeout(() => setToasts((cur) => cur.filter((x) => x.id !== n.id)), 4000);
    });
  }, [services]);
  if (toasts.length === 0) return null;
  return (
    <div className="toast-stack">
      {toasts.map((t) => (
        <button
          key={t.id}
          className="toast"
          onClick={() => {
            setToasts((cur) => cur.filter((x) => x.id !== t.id));
            onOpen(t.conversationId);
          }}
        >
          <strong>{t.title}</strong>
          <span>{t.preview}</span>
        </button>
      ))}
    </div>
  );
}

/** StorageWarning appears only when the local database could not be opened on
 *  persistent storage — normally because another tab of this app already holds
 *  it (the OPFS pool is exclusive to one tab). Without this the tab looks
 *  perfectly healthy and then loses everything on reload, which is exactly the
 *  failure it is here to prevent. */
function StorageWarning() {
  const { services } = useServices();
  const [warn, setWarn] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  useEffect(() => {
    let alive = true;
    services
      .storageDurable()
      .then((ok) => {
        if (alive) setWarn(!ok);
      })
      .catch(() => {
        /* the DB failing to answer is its own, louder problem */
      });
    return () => {
      alive = false;
    };
  }, [services]);
  if (!warn || dismissed) return null;
  return (
    <div className="storage-warning" role="status">
      <Icon name="info" size={18} />
      <span>
        This tab can’t save messages — the chat history is already open in another tab. Close the others and reload to keep
        new messages.
      </span>
      <button className="btn btn-ghost btn-sm" onClick={() => setDismissed(true)}>
        Dismiss
      </button>
    </div>
  );
}

type Nav =
  | { name: "login" }
  | { name: "verify"; challengeId: string; phone: string }
  | { name: "chats" }
  | { name: "newChat" }
  | { name: "search"; conversationId?: string; conversationTitle?: string }
  | { name: "calls" }
  | { name: "status" }
  | { name: "profile" }
  | { name: "settings" }
  | { name: "contacts" }
  | { name: "createGroup" }
  | { name: "groupInfo"; conversationId: string }
  | { name: "channels" }
  | { name: "channel"; channelId: string }
  | { name: "communities" }
  | { name: "community"; communityId: string }
  | { name: "collab"; conversationId: string }
  | { name: "board"; conversationId: string }
  | { name: "discover" }
  | { name: "thread"; conversationId: string; focusMsgUuid?: string };

/** NavRail is WhatsApp Web's far-left icon column: sections at the top, settings
 *  and your profile at the bottom. On narrow screens CSS turns it into a bottom
 *  tab bar. */
function NavRail({ active, go }: { active: string; go: (n: Nav) => void }) {
  const item = (key: string, label: string, icon: IconName, target: Nav) => (
    <button
      className={`rail-item${active === key ? " active" : ""}`}
      title={label}
      aria-label={label}
      aria-current={active === key ? "page" : undefined}
      onClick={() => go(target)}
    >
      <Icon name={icon} size={24} />
      <span className="rail-label">{label}</span>
    </button>
  );
  return (
    <nav className="wa-rail" aria-label="Sections">
      <div className="wa-rail-top">
        {item("chats", "Chats", "chats", { name: "chats" })}
        {item("updates", "Updates", "updates", { name: "status" })}
        {item("channels", "Channels", "channel", { name: "channels" })}
        {item("communities", "Communities", "community", { name: "communities" })}
        {item("calls", "Calls", "phone", { name: "calls" })}
      </div>
      <div className="wa-rail-bottom">
        {item("settings", "Settings", "settings", { name: "settings" })}
        <button
          className={`rail-item rail-avatar${active === "profile" ? " active" : ""}`}
          title="Profile"
          aria-label="Profile"
          onClick={() => go({ name: "profile" })}
        >
          <Avatar size={30} />
        </button>
      </div>
    </nav>
  );
}

/** Screens that take over the whole window on mobile: inside a conversation or a
 *  canvas, the bottom tab bar would steal space and invite mis-taps. Every other
 *  screen keeps the tabs so sections stay reachable on a phone. */
const IMMERSIVE = new Set<Nav["name"]>(["thread", "board", "collab"]);

/** railSectionOf maps a nav state to the rail section it belongs to, so the rail
 *  highlights correctly even for sub-screens (a thread lives under Chats). */
function railSectionOf(name: Nav["name"]): string {
  if (["chats", "thread", "newChat", "createGroup", "groupInfo", "contacts", "search", "collab", "board", "discover"].includes(name)) return "chats";
  if (name === "status") return "updates";
  if (name === "channels" || name === "channel") return "channels";
  if (name === "communities" || name === "community") return "communities";
  if (name === "calls") return "calls";
  if (name === "settings") return "settings";
  if (name === "profile") return "profile";
  return "chats";
}

/** Welcome fills the detail pane when no chat is open (desktop two-pane), the
 *  WhatsApp Web "select a chat" placeholder. */
function Welcome() {
  return (
    <div className="wa-welcome">
      <div className="wa-welcome-icon" aria-hidden>
        <Icon name="chats" size={44} />
      </div>
      <h1>WhatsApp V2</h1>
      <p>Select a chat to start messaging, or use the toolbar to start a new conversation.</p>
      <span className="wa-welcome-hint">
        <Icon name="info" size={15} /> Your messages are end-to-end encrypted
      </span>
    </div>
  );
}

function Router() {
  const { authed, services } = useServices();
  const [nav, setNav] = useState<Nav>(() => (authed ? { name: "chats" } : { name: "login" }));

  // A lost session (logout or a failed token refresh) bounces to login, so an
  // expired session doesn't strand the user on cryptic 401s (e.g. media uploads).
  useEffect(() => {
    if (!authed) setNav({ name: "login" });
  }, [authed]);

  // Auth flow is full-window (no shell).
  if (nav.name === "login") {
    return <Login onRequested={(challengeId, phone) => setNav({ name: "verify", challengeId, phone })} />;
  }
  if (nav.name === "verify") {
    return <Verify challengeId={nav.challengeId} phone={nav.phone} onDone={() => setNav({ name: "chats" })} />;
  }

  // Authed: a persistent chat list on the left, the active screen on the right.
  const detail = ((): JSX.Element => {
    switch (nav.name) {
      case "collab":
        return <CollabScreen conversationId={nav.conversationId} onBack={() => setNav({ name: "thread", conversationId: nav.conversationId })} />;
      case "board":
        return <WhiteboardScreen conversationId={nav.conversationId} onBack={() => setNav({ name: "thread", conversationId: nav.conversationId })} />;
      case "discover":
        return (
          <DiscoverScreen
            onBack={() => setNav({ name: "chats" })}
            onOpenChannel={(channelId) => setNav({ name: "channel", channelId })}
            onOpenCommunity={(communityId) => setNav({ name: "community", communityId })}
          />
        );
      case "thread":
        return (
          <Thread
            conversationId={nav.conversationId}
            focusMsgUuid={nav.focusMsgUuid}
            onBack={() => setNav({ name: "chats" })}
            onGroupInfo={(conversationId) => setNav({ name: "groupInfo", conversationId })}
            onCollab={(conversationId) => setNav({ name: "collab", conversationId })}
            onBoard={(conversationId) => setNav({ name: "board", conversationId })}
            onSearchInChat={(conversationId) =>
              setNav({
                name: "search",
                conversationId,
                conversationTitle: services.groupNameOf(conversationId) || services.peerNameOf(conversationId) || undefined,
              })
            }
          />
        );
      case "profile":
        return <Profile onBack={() => setNav({ name: "chats" })} onSettings={() => setNav({ name: "settings" })} />;
      case "settings":
        return <Settings onBack={() => setNav({ name: "profile" })} onSignedOut={() => setNav({ name: "login" })} />;
      case "status":
        return <Status onBack={() => setNav({ name: "chats" })} />;
      case "channels":
        return <Channels onOpen={(channelId) => setNav({ name: "channel", channelId })} onBack={() => setNav({ name: "chats" })} />;
      case "channel":
        return <ChannelScreen channelId={nav.channelId} onBack={() => setNav({ name: "channels" })} />;
      case "communities":
        return <Communities onOpen={(communityId) => setNav({ name: "community", communityId })} onBack={() => setNav({ name: "chats" })} />;
      case "community":
        return (
          <CommunityScreen
            communityId={nav.communityId}
            onBack={() => setNav({ name: "communities" })}
            onOpenGroup={(conversationId) => setNav({ name: "thread", conversationId })}
          />
        );
      case "calls":
        return <CallHistory onBack={() => setNav({ name: "chats" })} />;
      case "createGroup":
        return <CreateGroup onCreated={(conversationId) => setNav({ name: "thread", conversationId })} onBack={() => setNav({ name: "chats" })} />;
      case "groupInfo":
        return (
          <GroupInfoScreen
            conversationId={nav.conversationId}
            onBack={() => setNav({ name: "thread", conversationId: nav.conversationId })}
            onLeft={() => setNav({ name: "chats" })}
          />
        );
      case "contacts":
        return <Contacts onOpen={(conversationId) => setNav({ name: "thread", conversationId })} onBack={() => setNav({ name: "chats" })} />;
      case "newChat":
        return <NewChat onStarted={(conversationId) => setNav({ name: "thread", conversationId })} onBack={() => setNav({ name: "chats" })} />;
      case "search": {
        const scope = nav.conversationId;
        return (
          <Search
            conversationId={scope}
            conversationTitle={nav.conversationTitle}
            onOpen={(conversationId, focusMsgUuid) => setNav({ name: "thread", conversationId, focusMsgUuid })}
            onBack={() => (scope ? setNav({ name: "thread", conversationId: scope }) : setNav({ name: "chats" }))}
          />
        );
      }
      default:
        return <Welcome />;
    }
  })();

  return (
    <>
      <div className={`wa-shell${nav.name !== "chats" ? " show-detail" : ""}${IMMERSIVE.has(nav.name) ? " immersive" : ""}`}>
        <NavRail active={railSectionOf(nav.name)} go={setNav} />
        <div className="wa-panes">
          <aside className="wa-side">
            <ChatList
              activeId={nav.name === "thread" ? nav.conversationId : undefined}
              onOpen={(conversationId) => setNav({ name: "thread", conversationId })}
              onNew={() => setNav({ name: "newChat" })}
              onContacts={() => setNav({ name: "contacts" })}
              onNewGroup={() => setNav({ name: "createGroup" })}
              onDiscover={() => setNav({ name: "discover" })}
            />
          </aside>
          <section className="wa-detail">{detail}</section>
        </div>
      </div>
      <NotificationToasts onOpen={(conversationId) => setNav({ name: "thread", conversationId })} />
    </>
  );
}

export function App() {
  return (
    <ServicesProvider>
      <MediaProvider>
        <CallProvider>
          <div className="app">
            <Router />
            <StorageWarning />
            <CallOverlay />
          </div>
        </CallProvider>
      </MediaProvider>
    </ServicesProvider>
  );
}
