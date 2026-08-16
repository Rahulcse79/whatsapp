// Breakout rooms — client side (T9.03). The server owns assignment; the client
// polls its current room (GET /v1/live/{id}/me) and this controller rejoins the
// LiveKit room whenever the host moves it into or out of a breakout. Pure state
// machine over an injected RoomJoiner (LiveKit connect); framework-free.

export interface RoomAssignment {
  /** breakout room id, or null for the main room */
  roomId: string | null;
  /** LiveKit room name to join */
  room: string;
  /** fresh join token for that room */
  joinToken: string;
}

export interface BreakoutState {
  roomId: string | null;
  room: string | null;
}

/** RoomJoiner is the LiveKit seam: leave the current room and connect to `room`
 *  with `token`. A single call re-points the media connection. */
export interface RoomJoiner {
  join(room: string, token: string): Promise<void>;
}

export class BreakoutController {
  private state: BreakoutState = { roomId: null, room: null };
  private queue: Promise<void> = Promise.resolve();

  constructor(
    private readonly joiner: RoomJoiner,
    private readonly onChange?: (s: BreakoutState) => void,
  ) {}

  getState(): BreakoutState {
    return this.state;
  }

  /** apply a fresh assignment (from GET /me). Rejoins only when the LiveKit room
   *  actually changes, so repeated polls are no-ops. */
  apply(next: RoomAssignment): Promise<void> {
    return this.run(async () => {
      if (this.state.room === next.room) return;
      await this.joiner.join(next.room, next.joinToken);
      this.set({ roomId: next.roomId, room: next.room });
    });
  }

  private set(patch: Partial<BreakoutState>): void {
    this.state = { ...this.state, ...patch };
    this.onChange?.(this.state);
  }

  private run(op: () => Promise<void>): Promise<void> {
    this.queue = this.queue.then(op, op);
    return this.queue;
  }
}
