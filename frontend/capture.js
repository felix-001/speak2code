class VoiceCapture extends AudioWorkletProcessor {
  constructor() {
    super();
    this.samples = new Float32Array(960);
    this.offset = 0;
    this.active = true;
    this.elapsed = 0;
    this.quiet = 0;
    this.heardVoice = false;
    this.port.onmessage = () => {
      this.active = false;
      if (this.offset) this.port.postMessage(this.samples.subarray(0, this.offset));
      this.port.postMessage('stopped');
    };
  }
  process(inputs) {
    if (!this.active) return false;
    const input = inputs[0]?.[0];
    if (input) {
      let energy = 0;
      for (const sample of input) energy += sample * sample;
      const duration = input.length / sampleRate;
      this.elapsed += duration;
      if (Math.sqrt(energy / input.length) >= 0.015) {
        this.heardVoice = true;
        this.quiet = 0;
      } else {
        this.quiet += duration;
      }
    }
    if (input) for (const sample of input) {
      this.samples[this.offset++] = sample;
      if (this.offset === 960) {
        this.port.postMessage(this.samples);
        this.samples = new Float32Array(960);
        this.offset = 0;
      }
    }
    if ((this.heardVoice && this.quiet >= 1.5) || (!this.heardVoice && this.elapsed >= 10) || this.elapsed >= 60) {
      this.active = false;
      this.port.postMessage('silence');
    }
    return true;
  }
}
registerProcessor('voice-capture', VoiceCapture);
