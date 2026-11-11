// A bounded series of plot points for one port: parallel plain arrays, because uPlot 1.6.32 breaks
// a line only at `null` and a Float64Array cannot hold one (Research Pitfall 4). Values are what the
// server sent: x is the sample's sim time in seconds, rpm as reported, duty in percent (the API's
// 0..1 times 100), tach as reported (the last 1 s gate reading of the port's tach). The four arrays
// are parallel to xs. The ring never resamples, smooths or interpolates, and it derives no gap from
// timing: a gap is a null value the server sent, or the `gap` marker the server put on the first
// sample of an engine.
export class Ring {
  // cap is how many points are kept once the ring is trimmed, slack how far it may grow beyond
  // cap before the oldest points are dropped (so a trim happens once per `slack` pushes).
  constructor(cap, slack) {
    this.cap = cap;
    this.slack = slack;
    this.xs = [];
    this.rpm = [];
    this.duty = [];
    this.tach = [];
    this.seen = false;
  }

  // push appends one sample: t in microseconds of sim time, rpm, duty (0..1) and tach or
  // null/undefined when the firmware was off the bus or the port has no reading, gap true when the
  // server marked it as the first sample of an engine. tach comes last, so callers that pass four
  // arguments keep working. A marked sample is preceded by one null point at its own x (when the
  // ring holds a point), which makes uPlot break all the lines before it; the page never compares
  // sample times.
  push(t, rpm, duty, gap, tach) {
    const x = t / 1e6;
    if (gap === true && this.xs.length > 0) {
      this.add(x, null, null, null);
    }
    const r = typeof rpm === "number" ? rpm : null;
    const d = typeof duty === "number" ? duty * 100 : null;
    const k = typeof tach === "number" ? tach : null;
    if (r !== null || d !== null || k !== null) {
      this.seen = true;
    }
    this.add(x, r, d, k);
    if (this.xs.length > this.cap + this.slack) {
      const drop = this.xs.length - this.cap;
      this.xs.splice(0, drop);
      this.rpm.splice(0, drop);
      this.duty.splice(0, drop);
      this.tach.splice(0, drop);
    }
  }

  add(x, rpm, duty, tach = null) {
    this.xs.push(x);
    this.rpm.push(rpm);
    this.duty.push(duty);
    this.tach.push(tach);
  }

  // clear forgets every point, in place (uPlot holds these arrays).
  clear() {
    this.xs.length = 0;
    this.rpm.length = 0;
    this.duty.length = 0;
    this.tach.length = 0;
    this.seen = false;
  }

  get length() {
    return this.xs.length;
  }
}
