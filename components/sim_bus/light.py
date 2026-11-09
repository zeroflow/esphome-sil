import re

import esphome.codegen as cg
from esphome.components import light
import esphome.config_validation as cv
from esphome.const import (
    CONF_FROM,
    CONF_ID,
    CONF_NUM_LEDS,
    CONF_OUTPUT_ID,
    CONF_SEGMENTS,
    CONF_TO,
)
from esphome.core import CORE
import esphome.final_validate as fv

from . import CHILD_SCHEMA, register_child, sim_bus_ns

SimLight = sim_bus_ns.class_("SimLight", light.AddressableLight)
CONF_SEGMENT_LABELS = "segment_labels"

# A bus line is at most 4095 bytes; 255 LEDs are 510 hex characters of pixels.
MAX_LEDS = 255
# The derived seg travels in one chan frame (4095 bytes max); 2048 leaves room for the rest.
SEG_MAX_BYTES = 2048
LABEL_RE = re.compile(r"^[a-z][a-z0-9_]*$")


def validate_label(value):
    """Segment label as it appears in `sil status`: ^[a-z][a-z0-9_]*$."""
    value = cv.string(value)
    if not LABEL_RE.match(value):
        raise cv.Invalid(f"invalid segment label '{value}': must match ^[a-z][a-z0-9_]*$")
    return value


CONFIG_SCHEMA = (
    light.ADDRESSABLE_LIGHT_SCHEMA.extend(
        {
            cv.GenerateID(CONF_OUTPUT_ID): cv.declare_id(SimLight),
            cv.Required(CONF_NUM_LEDS): cv.int_range(min=1, max=MAX_LEDS),
            # partition light id -> label shown in `sil status` (default: the id minus a trailing `_led`)
            cv.Optional(CONF_SEGMENT_LABELS, default={}): cv.Schema({cv.string: validate_label}),
        }
    )
    .extend(CHILD_SCHEMA)
    .extend(cv.COMPONENT_SCHEMA)
)


def _ident(value):
    return getattr(value, "id", None) or str(value)


def partitions_of(light_config, full_config):
    """(partition id, from, to) of every stock partition light that has this light as a segment
    source, in the order of the config. The partition's own from/to are the single source of truth."""
    me = _ident(light_config[CONF_ID])
    out = []
    lights = full_config.get("light") or []
    if isinstance(lights, dict):
        lights = [lights]
    for entry in lights:
        if not isinstance(entry, dict) or entry.get("platform") != "partition":
            continue
        for seg in entry.get(CONF_SEGMENTS) or []:
            if seg.get(CONF_ID) is not None and _ident(seg[CONF_ID]) == me:
                out.append((_ident(entry[CONF_ID]), seg[CONF_FROM], seg[CONF_TO]))
    return out


def default_label(pid, labels):
    """The label of a partition: segment_labels wins, else the id minus a trailing `_led`."""
    label = labels.get(pid)
    if label is None:
        label = pid[: -len("_led")] if pid.endswith("_led") else pid
    return label


def derive_segments(light_config, full_config):
    """`label:from:to,...` in partition config order (from and to inclusive). The label is the
    partition id minus a trailing `_led`, unless `segment_labels` names it."""
    labels = light_config.get(CONF_SEGMENT_LABELS) or {}
    out = []
    for pid, lo, hi in partitions_of(light_config, full_config):
        label = default_label(pid, labels)
        out.append(f"{label}:{lo}:{hi}")
    return ",".join(out)


def validate_segments(config):
    """Config-time consistency of this light and its partition segments (D-06: the YAML is the
    single source of truth, so a mistake stops `esphome config` instead of reaching the bus).

    Rejected, each naming the light and the offending entry: a segment_labels key that is not a
    partition of this light, a label that is not ^[a-z][a-z0-9_]*$, two partitions with the same
    label, a partition segment outside 0..num_leds-1, and a derived seg longer than SEG_MAX_BYTES.
    A light that no partition uses is valid (empty seg)."""
    full = fv.full_config.get()
    me = _ident(config[CONF_ID])
    num_leds = config[CONF_NUM_LEDS]
    labels = config.get(CONF_SEGMENT_LABELS) or {}
    parts = partitions_of(config, full)
    pids = {p[0] for p in parts}
    for pid in labels:
        if pid not in pids:
            raise cv.Invalid(
                f"light '{me}': segment_labels names '{pid}', which is not a partition light of '{me}'"
            )
    seen = {}
    for pid, lo, hi in parts:
        if lo > hi or hi >= num_leds:
            raise cv.Invalid(
                f"light '{me}': partition '{pid}' covers LEDs {lo}..{hi}, outside the strip "
                f"0..{num_leds - 1} (num_leds: {num_leds})"
            )
        label = default_label(pid, labels)
        if not LABEL_RE.match(label):
            raise cv.Invalid(
                f"light '{me}': segment label '{label}' of partition '{pid}' is not valid "
                "([a-z][a-z0-9_]*); name it with segment_labels"
            )
        if label in seen:
            raise cv.Invalid(
                f"light '{me}': duplicate segment label '{label}' (partitions '{seen[label]}' and "
                f"'{pid}'); name one of them with segment_labels"
            )
        seen[label] = pid
    seg = derive_segments(config, full)
    if len(seg.encode()) > SEG_MAX_BYTES:
        raise cv.Invalid(
            f"light '{me}': the derived seg is {len(seg.encode())} bytes, over the limit of "
            f"{SEG_MAX_BYTES}; shorten the partition labels or use fewer partitions"
        )
    return config


FINAL_VALIDATE_SCHEMA = validate_segments


async def to_code(config):
    var = cg.new_Pvariable(config[CONF_OUTPUT_ID])
    await light.register_light(var, config)
    await cg.register_component(var, config)
    cg.add(var.set_num_leds(config[CONF_NUM_LEDS]))
    cg.add(var.set_segments(derive_segments(config, CORE.config)))
    await register_child(var, config)
