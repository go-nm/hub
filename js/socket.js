let ws;
let timeout = 1000;

class Channel {
  ws = null;
  isJoined = false;
  topic = '';
  channel = '';

  constructor(ws, fullTopic) {
    this.ws = ws;

    const [topic, chan] = fullTopic.split(':');
    this.topic = topic;
    this.channel = chan;
  }

  join() {
    if (this.isJoined) {
      return false;
    }

    this.ws.send(JSON.stringify({
      topic: this.topic,
      event: 'join_topic',
    }));
  }
}

class Socket {
  ws = null;
  url = '';
  opts = {
    timeout: 1000,
    onOpen: e => null,
    onError: e => null,
    onClose: e => null,
  };
  currentTimeout = 0;
  channels = {};

  constructor(url, opts) {
    this.url = url;
    this.opts = {...this.opts, ...opts};
    this.currentTimeout = this.opts.timeout;

    this.connect = this.connect.bind(this);
    this.onOpen = this.onOpen.bind(this);
    this.onClose = this.onClose.bind(this);
    this.onMessage = this.onMessage.bind(this);
    this.onError = this.onError.bind(this);

    this.connect();
  }

  connect() {
    this.ws = new WebSocket(this.url);
    this.ws.addEventListener('open', this.onOpen);
    this.ws.addEventListener('close', this.onClose);
    this.ws.addEventListener('message', this.onMessage);
    this.ws.addEventListener('error', this.onError);
  }

  join(channelName) {
    const channel = new Channel(this.ws, channelName);
    this.channels[channelName] = channel;
    channel.join();
    return channel;
  }

  onOpen(e) {
    console.info('connected to socket at:', this.url);

    this.currentTimeout = this.opts.timeout;

    if (this.opts.onOpen) {
      this.opts.onOpen(e);
    }
  }

  onClose(e) {
    if (!e.wasClean) {
      console.error('Socket closed:', e);
      setTimeout(this.connect, this.currentTimeout);
      this.currentTimeout = this.currentTimeout * 1.5;
    }

    if (this.opts.onClose) {
      this.opts.onClose(e);
    }
  }

  onError(e) {
    console.error('Socket error:', e)

    if (this.opts.onError) {
      this.opts.onError(e);
    }
  }

  onMessage(e) {
    const msg = JSON.parse(e.data);
    console.log(msg);
    this.channels[msg.topic]
  }
}
