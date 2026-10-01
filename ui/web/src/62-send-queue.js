  // ---- messages sent while a turn runs -------------------------------------
  //
  // Enter during a turn used to stop it: the button was Stop and a typed
  // message threw the work away. Now a message sent while a turn runs goes to
  // that turn (session.interject), and the model reads it at its next step; a
  // turn that cannot take it any more (past its last step, an older core, a
  // message with attachments) leaves it here, and it goes out as the next turn.
  // The queue is the renderer's (05c-busy-palette.js, queueUpdate): an item the
  // turn took is locked — it cannot be called back — and leaves the queue when
  // the user_message event says the model got it.

  let sendQueueSeq = 0;

  /** @param {string} projectId */
  function sendQueueOf(projectId) {
    const st = projectState(projectId);
    if (!st.sendQueue) {
      st.sendQueue = [];
      st.deliveredEarly = new Set();
    }
    return st.sendQueue;
  }

  /** @param {string} projectId */
  function postSendQueue(projectId) {
    if (projectId !== currentProjectId) {
      return;
    }
    toRenderer({
      type: "queueUpdate",
      items: sendQueueOf(projectId).map((it) => ({
        id: it.id,
        preview: it.text,
        fileCount: it.files.length,
        locked: Boolean(it.coreId),
      })),
    });
  }

  /**
   * A send while this project's turn runs.
   * @param {string} projectId @param {any} msg
   */
  async function sendWhileBusy(projectId, msg) {
    const st = projectState(projectId);
    const item = {
      id: "q" + ++sendQueueSeq,
      text: String(msg.text || ""),
      files: Array.isArray(msg.files) ? msg.files : [],
      msg,
      coreId: "",
    };
    const queue = sendQueueOf(projectId);
    queue.push(item);
    postSendQueue(projectId);
    // session.interject carries text: a message with attachments waits for a
    // turn of its own.
    if (item.files.length || !item.text.trim() || !st.sessionId) {
      return;
    }
    let res = null;
    try {
      res = await connFor(projectId).send("session.interject", { session_id: st.sessionId, content: item.text });
    } catch (err) {
      return; // an older core: the message waits for the next turn
    }
    if (!res || !res.accepted || !res.id || !queue.includes(item)) {
      return;
    }
    const id = String(res.id);
    // The model can reach its next step before this answer is read here.
    if (st.deliveredEarly.has(id)) {
      st.deliveredEarly.delete(id);
      queue.splice(queue.indexOf(item), 1);
    } else {
      item.coreId = id;
    }
    postSendQueue(projectId);
  }

  /**
   * The user_message event: a message the turn took reached the model. It goes
   * into the chat where the model read it.
   * @param {string} projectId @param {any} ev
   */
  function noteInterjectionDelivered(projectId, ev) {
    const st = projectState(projectId);
    const queue = sendQueueOf(projectId);
    const id = ev && ev.data && ev.data.id ? String(ev.data.id) : "";
    const i = queue.findIndex((it) => it.coreId !== "" && it.coreId === id);
    if (i >= 0) {
      queue.splice(i, 1);
    } else if (id) {
      st.deliveredEarly.add(id);
    }
    if (projectId === currentProjectId) {
      toRenderer({ type: "userInterjection", text: String((ev && ev.content) || "") });
      postSendQueue(projectId);
    }
  }

  /** @param {string} projectId @param {string} id */
  function cancelQueuedSend(projectId, id) {
    const queue = sendQueueOf(projectId);
    const i = queue.findIndex((it) => it.id === id && it.coreId === "");
    if (i >= 0) {
      queue.splice(i, 1);
      postSendQueue(projectId);
    }
  }

  /**
   * After a turn: what it never took waits no longer. Stopped by the person,
   * it goes back into the composer; otherwise it goes out as one next turn
   * once `saved` (the answer written into the session) has settled, so the two
   * writes to the chat cannot cross. Reports whether a next turn is coming.
   * @param {string} projectId @param {boolean} stopped @param {Promise<any>} saved
   */
  function drainSendQueue(projectId, stopped, saved) {
    const queue = sendQueueOf(projectId);
    for (const it of queue) {
      it.coreId = "";
    }
    if (!queue.length) {
      return false;
    }
    if (stopped || projectId !== currentProjectId) {
      // A background project keeps its queue until it is on screen again.
      if (stopped) {
        const text = queue.map((it) => it.text).filter(Boolean).join("\n\n");
        queue.length = 0;
        if (text && projectId === currentProjectId) {
          toRenderer({ type: "restoreDraft", text });
        }
      }
      postSendQueue(projectId);
      return false;
    }
    const items = queue.splice(0);
    postSendQueue(projectId);
    const next = Object.assign({}, items[0].msg, {
      text: items.map((it) => it.text).filter(Boolean).join("\n\n"),
      files: items.flatMap((it) => it.files),
    });
    void Promise.resolve(saved)
      .catch(() => undefined)
      .then(() => {
        if (projectId === currentProjectId && projectState(projectId).inFlightTurnId === null) {
          void sendTurn(next);
        } else {
          queue.push(...items);
          postSendQueue(projectId);
        }
      });
    return true;
  }
