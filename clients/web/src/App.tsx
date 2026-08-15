import { useState } from "react";
import { CallOverlay } from "./call/CallOverlay";
import { CallProvider } from "./call/CallContext";
import { MediaProvider } from "./ui/media/MediaContext";
import { CallHistory, ChatList, Contacts, CreateGroup, GroupInfoScreen, Login, NewChat, Profile, Search, Settings, Status, Thread, Verify } from "./ui/screens";
import { ServicesProvider, useServices } from "./ui/ServicesContext";

type Nav =
  | { name: "login" }
  | { name: "verify"; challengeId: string; phone: string }
  | { name: "chats" }
  | { name: "newChat" }
  | { name: "search" }
  | { name: "calls" }
  | { name: "status" }
  | { name: "profile" }
  | { name: "settings" }
  | { name: "contacts" }
  | { name: "createGroup" }
  | { name: "groupInfo"; conversationId: string }
  | { name: "thread"; conversationId: string };

function Router() {
  const { authed } = useServices();
  const [nav, setNav] = useState<Nav>(() => (authed ? { name: "chats" } : { name: "login" }));

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
    return (
      <Search
        onOpen={(conversationId) => setNav({ name: "thread", conversationId })}
        onBack={() => setNav({ name: "chats" })}
      />
    );
  }
  // nav.name === "thread"
  return (
    <Thread
      conversationId={nav.conversationId}
      onBack={() => setNav({ name: "chats" })}
      onGroupInfo={(conversationId) => setNav({ name: "groupInfo", conversationId })}
    />
  );
}

export function App() {
  return (
    <ServicesProvider>
      <MediaProvider>
        <CallProvider>
          <div className="app">
            <header className="topbar">WhatsApp V2</header>
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
