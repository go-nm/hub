class Room {
  ws = null;
  isJoined = false;
  topic = '';
  room = '';
  eventHandlers = {};

  static AlreadyJoinedError = new Error('Already Joined');
  static NotJoinedError = new Error('Not Joined');

  constructor(ws, fullName) {
    this.ws = ws;

    const [topic, room] = fullName.split(':');
    this.topic = topic;
    this.room = room;
  }

  join() {
    if (this.isJoined) {
      throw Room.AlreadyJoinedError;
    }

    this.ws.send(JSON.stringify({
      topic: this.topic,
      room: this.room,
      event: 'join',
    }));
  }

  send(event, payload) {
    if (!this.isJoined) {
      throw Room.NotJoinedError;
    }

    this.ws.send(JSON.stringify({
      event,
      payload,
      topic: this.topic,
      room: this.room,
    }));
  }

  leave(payload) {
    this.ws.send(JSON.stringify({
      payload,
      topic: this.topic,
      room: this.room,
      event: 'leave',
    }));
  }

  onEvent(event, handler) {
    if (!this.eventHandlers[event]) {
      this.eventHandlers[event] = [];
    }

    this.eventHandlers[event].push(handler);
  }

  messageReceived(event, payload) {
    // Handle system generated messages
    switch (event) {
      case 'joined':
        this.isJoined = true;
        break;

      case 'left':
        this.isJoined = false;
        break;

      default:
        break;
    }

    if (this.eventHandlers[event]) {
      this.eventHandlers[event].forEach(h => h(event, payload));
    } else {
      console.warn(`Event (${event}) not being watched`)
    }
  }
}

class Socket {
  ws = null;
  url = '';
  opts = {
    timeout: 1000,
  };
  currentTimeout = 0;
  rooms = {};
  openHandlers = [];
  closeHandlers = [];
  errorHandlers = [];

  constructor(url, opts) {
    this.url = url;
    this.opts = {...this.opts, ...opts};
    this.currentTimeout = this.opts.timeout;

    this.connect = this.connect.bind(this);
    this.onOpen = this.onOpen.bind(this);
    this.onClose = this.onClose.bind(this);
    this.onError = this.onError.bind(this);
  }

  connect() {
    this.ws = new WebSocket(this.url);
    this.ws.addEventListener('open', e => {
      console.info('connected to socket at:', this.url);

      this.currentTimeout = this.opts.timeout;

      // Call all the user defined callbacks
      this.openHandlers.forEach(fn => fn(e));
    });

    this.ws.addEventListener('close', e => {
      if (!e.wasClean) {
        console.error('Socket closed:', e);
        setTimeout(this.connect, this.currentTimeout);
        this.currentTimeout = this.currentTimeout * 1.5;
      }

      // Call all the user defined callbacks
      this.closeHandlers.forEach(fn => fn(e));
    });

    this.ws.addEventListener('message', e => {
      const msg = JSON.parse(e.data);

      const channel = msg.topic+":"+msg.room;
      if (this.rooms[channel]) {
        this.rooms[channel].messageReceived(msg.event, msg.payload);
      } else {
        console.warn(`Channel (${channel}) not found for message:`, msg);
      }
    });

    this.ws.addEventListener('error', e => {
      console.error('Socket error:', e);

      // Call all the user defined callbacks
      this.errorHandlers.forEach(fn => fn(e));
    });
  }

  join(roomName) {
    const room = new Room(this.ws, roomName);
    this.rooms[roomName] = room;
    room.join();
    return room;
  }

  onOpen(fn) {
    this.openHandlers.push(fn);
  }

  onClose(fn) {
    this.closeHandlers.push(fn);
  }

  onError(fn) {
    this.errorHandlers.push(fn);
  }
}
