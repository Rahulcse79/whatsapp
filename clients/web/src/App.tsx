import { useEffect, useState } from "react";
import { CallOverlay } from "./call/CallOverlay";
import { CallProvider } from "./call/CallContext";
import type { NotificationEntry } from "./services/appServices";
import { MediaProvider } from "./ui/media/MediaContext";
import { CallHistory, ChatList, Contacts, CreateGroup, GroupInfoScreen, Login, NewChat, Profile, Search, Settings, Status, Thread, Verify } from "./ui/screens";
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
  | { name: "thread"; conversationId: string; focusMsgUuid?: string };

function Router() {
  const { authed, services } = useServices();
  const [nav, setNav] = useState<Nav>(() => (authed ? { name: "chats" } : { name: "login" }));

  const body = ((): JSX.Element => {
  if (nav.name === "login") {
    return <Login onRequested={(challengeId, phone) => setNav({ name: "verify", challengeId, phone })} />;
  }
  if (nav.name === "verify") {
    return <Verify challengeId={nav.challengeId} phone={nav.phone} onDone={() => setNav({ name: "chats" })} />;
  }
  if (nav.name === "chats") {
    return (
      <ChatList
        onOpen={(conversationId) => setNav({ name: "thread", conversationId })}
        onNew={() => setNav({ name: "newChat" })}
        onSearch={() => setNav({ name: "search" })}
        onProfile={() => setNav({ name: "profile" })}
        onContacts={() => setNav({ name: "contacts" })}
        onNewGroup={() => setNav({ name: "createGroup" })}
        onCalls={() => setNav({ name: "calls" })}
        onStatus={() => setNav({ name: "status" })}
      />
    );
  }
  if (nav.name === "profile") {
    return <Profile onBack={() => setNav({ name: "chats" })} onSettings={() => setNav({ name: "settings" })} />;
  }
  if (nav.name === "settings") {
    return <Settings onBack={() => setNav({ name: "profile" })} onSignedOut={() => setNav({ name: "login" })} />;
  }
  if (nav.name === "status") {
    return <Status onBack={() => setNav({ name: "chats" })} />;
  }
  if (nav.name === "calls") {
    return <CallHistory onBack={() => setNav({ name: "chats" })} />;
  }
  if (nav.name === "createGroup") {
    return (
      <CreateGroup
        onCreated={(conversationId) => setNav({ name: "thread", conversationId })}
        onBack={() => setNav({ name: "chats" })}
      />
    );
  }
  if (nav.name === "groupInfo") {
    return (
      <GroupInfoScreen
        conversationId={nav.conversationId}
        onBack={() => setNav({ name: "thread", conversationId: nav.conversationId })}
        onLeft={() => setNav({ name: "chats" })}
      />
    );
  }
  if (nav.name === "contacts") {
    return (
      <Contacts
        onOpen={(conversationId) => setNav({ name: "thread", conversationId })}
        onBack={() => setNav({ name: "chats" })}
      />
    );
  }
  if (nav.name === "newChat") {
    return (
      <NewChat
        onStarted={(conversationId) => setNav({ name: "thread", conversationId })}
        onBack={() => setNav({ name: "chats" })}
      />
    );
  }
  if (nav.name === "search") {
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
  // nav.name === "thread"
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
  })();

  return (
    <>
      {body}
      {authed && <NotificationToasts onOpen={(conversationId) => setNav({ name: "thread", conversationId })} />}
    </>
  );
}

/** TopbarUnread shows the account-wide unread badge next to the app title. */
function TopbarUnread() {
  const { services } = useServices();
  const [n, setN] = useState(0);
  useEffect(() => {
    const tick = (): void => setN(services.totalUnread());
    tick();
    return services.onChange(tick);
  }, [services]);
  if (n === 0) return null;
  return <span className="topbar-badge">{n > 99 ? "99+" : n}</span>;
}

export function App() {
  return (
    <ServicesProvider>
      <MediaProvider>
        <CallProvider>
          <div className="app">
            <header className="topbar">
              WhatsApp V2 <TopbarUnread />
            </header>
            <main className="main">
              <Router />
            </main>
            <CallOverlay />
          </div>
        </CallProvider>
      </MediaProvider>
    </ServicesProvider>
  );
}
