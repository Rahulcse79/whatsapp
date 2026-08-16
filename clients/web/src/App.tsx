import { useEffect, useState } from "react";
import { CallOverlay } from "./call/CallOverlay";
import { CallProvider } from "./call/CallContext";
import type { NotificationEntry } from "./services/appServices";
import { MediaProvider } from "./ui/media/MediaContext";
import { CallHistory, ChannelScreen, Channels, ChatList, Communities, CommunityScreen, Contacts, CreateGroup, GroupInfoScreen, Login, NewChat, Profile, Search, Settings, Status, Thread, Verify } from "./ui/screens";
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
  | { name: "thread"; conversationId: string; focusMsgUuid?: string };

/** Welcome fills the detail pane when no chat is open (desktop two-pane), the
 *  WhatsApp Web "select a chat" placeholder. */
function Welcome() {
  return (
    <div className="wa-welcome">
      <div className="wa-welcome-icon" aria-hidden>
        💬
      </div>
      <h1>WhatsApp V2</h1>
      <p>Select a chat to start messaging, or use the toolbar to start a new conversation. Your messages are end-to-end encrypted.</p>
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
      case "thread":
        return (
          <Thread
            conversationId={nav.conversationId}
            focusMsgUuid={nav.focusMsgUuid}
            onBack={() => setNav({ name: "chats" })}
            onGroupInfo={(conversationId) => setNav({ name: "groupInfo", conversationId })}
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
      <div className={`wa-shell${nav.name !== "chats" ? " show-detail" : ""}`}>
        <aside className="wa-side">
          <ChatList
            activeId={nav.name === "thread" ? nav.conversationId : undefined}
            onOpen={(conversationId) => setNav({ name: "thread", conversationId })}
            onNew={() => setNav({ name: "newChat" })}
            onSearch={() => setNav({ name: "search" })}
            onProfile={() => setNav({ name: "profile" })}
            onContacts={() => setNav({ name: "contacts" })}
            onNewGroup={() => setNav({ name: "createGroup" })}
            onCalls={() => setNav({ name: "calls" })}
            onStatus={() => setNav({ name: "status" })}
            onChannels={() => setNav({ name: "channels" })}
            onCommunities={() => setNav({ name: "communities" })}
          />
        </aside>
        <section className="wa-detail">{detail}</section>
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
            <CallOverlay />
          </div>
        </CallProvider>
      </MediaProvider>
    </ServicesProvider>
  );
}
