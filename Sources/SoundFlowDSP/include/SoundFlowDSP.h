#ifndef SOUNDFLOW_DSP_H
#define SOUNDFLOW_DSP_H
#include <stdint.h>
#include <stddef.h>
#define SF_MAX_STREAMS 16
#define SF_SAMPLE_RATE 48000
typedef struct SFMixer SFMixer;
SFMixer *sf_mixer_create(uint32_t target_frames);
void sf_mixer_destroy(SFMixer *mixer);
// A single network producer writes each slot; a single audio consumer renders.
size_t sf_mixer_write_s16le(SFMixer *mixer, uint32_t slot, const uint8_t *pcm, size_t frames);
void sf_mixer_flush(SFMixer *mixer, uint32_t slot);
void sf_mixer_set_gain(SFMixer *mixer, uint32_t slot, float gain);
void sf_mixer_set_master(SFMixer *mixer, float gain);
void sf_mixer_set_target(SFMixer *mixer, uint32_t frames);
uint32_t sf_mixer_buffered(SFMixer *mixer, uint32_t slot);
uint64_t sf_mixer_underruns(SFMixer *mixer, uint32_t slot);
// Planar stereo. No locks, allocations or system calls on the audio thread.
void sf_mixer_render(SFMixer *mixer, float *left, float *right, uint32_t frames);
#endif
