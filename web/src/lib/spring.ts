// A spring in Apple's designer terms: dampingRatio (1 = settles without overshoot, < 1 bounces) and
// response (seconds, roughly one period). It keeps position and velocity, so a new target or a grab
// mid-flight continues from what is on screen instead of jumping.

export const reducedMotion = () => matchMedia('(prefers-reduced-motion: reduce)').matches

export class Spring {
  value: number
  velocity = 0
  private target: number
  private frame = 0

  constructor(value: number, private onChange: (v: number) => void) {
    this.value = this.target = value
  }

  /** Animates to target. velocity (px/s) hands over a gesture's speed so drag and animation meet seamlessly. */
  to(target: number, { dampingRatio = 1, response = 0.35, velocity = this.velocity, onRest = () => {} } = {}) {
    this.target = target
    this.velocity = velocity
    cancelAnimationFrame(this.frame)
    if (reducedMotion()) return this.set(target), onRest()
    const k = (2 * Math.PI / response) ** 2
    const c = (4 * Math.PI * dampingRatio) / response
    let last = performance.now()
    const tick = (now: number) => {
      let dt = Math.min((now - last) / 1000, 1 / 30) // after a background tab, don't leap
      last = now
      for (; dt > 0; dt -= 1 / 240) { // fixed small steps keep stiff springs stable
        const h = Math.min(dt, 1 / 240)
        this.velocity += (-k * (this.value - this.target) - c * this.velocity) * h
        this.value += this.velocity * h
      }
      if (Math.abs(this.value - this.target) < 0.5 && Math.abs(this.velocity) < 5) return this.set(this.target), onRest()
      this.onChange(this.value)
      this.frame = requestAnimationFrame(tick)
    }
    this.frame = requestAnimationFrame(tick)
  }

  /** Jumps (e.g. 1:1 under a finger). Stops any animation. */
  set(v: number) {
    cancelAnimationFrame(this.frame)
    this.value = v
    this.velocity = 0
    this.onChange(v)
  }
}

/** Where a flick would come to rest, like scroll deceleration (Apple's projection). */
export const project = (velocity: number, decelerationRate = 0.998) =>
  ((velocity / 1000) * decelerationRate) / (1 - decelerationRate)

/** Progressive resistance past an edge: things slow before they stop. */
export const rubberband = (overshoot: number, dimension: number, c = 0.55) =>
  (overshoot * dimension * c) / (dimension + c * Math.abs(overshoot))
