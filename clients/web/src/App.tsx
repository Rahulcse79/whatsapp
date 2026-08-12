import { newId } from "@wa/client-core";
import { useState } from "react";
import { MediaProvider } from "./ui/media/MediaContext";
import { ChatList, Login, Search, Thread, Verify } from "./ui/screens";
import { ServicesProvider, useServices } from "./ui/ServicesContext";

type Nav =
  | { name: "login" }
  | { name: "verify"; challengeId: string; phone: string }
  | { name: "chats" }
  | { name: "search" }
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
        onNew={() => setNav({ name: "thread", conversationId: newId() })}
        onSearch={() => setNav({ name: "search" })}
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
  return <Thread conversationId={nav.conversationId} onBack={() => setNav({ name: "chats" })} />;
}

export function App() {
  return (
    <ServicesProvider>
      <MediaProvider>
        <div className="app">
          <header className="topbar">WhatsApp V2</header>
          <main className="main">
            <Router />
          </main>
        </div>
      </MediaProvider>
    </ServicesProvider>
  );
}
