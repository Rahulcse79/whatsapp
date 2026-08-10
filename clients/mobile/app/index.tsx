import { Redirect } from "expo-router";
import { useServices } from "../src/ui/ServicesContext";

// Entry gate: send signed-in users to the chat list, everyone else to sign-in.
export default function Index() {
  const { authed } = useServices();
  return <Redirect href={authed ? "/chats" : "/login"} />;
}
