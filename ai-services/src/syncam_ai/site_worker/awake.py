"""Keeps the site box's computer from going to sleep while the box runs.

A sleeping PC sends no alerts, and nothing wakes it up again. While the box
runs it holds a Windows power request ("system required"), the same mechanism
media players and backup tools use. It does not change the computer's power
settings, the screen can still turn off, and the request ends as soon as the
box stops. Windows lists it under ``powercfg /requests`` (run as
administrator) with the reason below.

It cannot stop someone choosing Sleep or Shut down, closing a laptop lid set to
"sleep", or a power cut. Set ``"keep_awake": false`` in the site config to
turn it off, for example on a laptop that should sleep on battery.
"""

from __future__ import annotations

import ctypes
import logging
import sys
from typing import Any

_LOG = logging.getLogger(__name__)

REASON = "SyncCam Box is watching the cameras and sending alerts"
POWER_REQUEST_CONTEXT_VERSION = 0
POWER_REQUEST_CONTEXT_SIMPLE_STRING = 0x1
POWER_REQUEST_SYSTEM_REQUIRED = 1
ES_CONTINUOUS = 0x80000000
ES_SYSTEM_REQUIRED = 0x00000001
INVALID_HANDLE_VALUE = ctypes.c_void_p(-1).value


class _Detailed(ctypes.Structure):
    _fields_ = [
        ("LocalizedReasonModule", ctypes.c_void_p),
        ("LocalizedReasonId", ctypes.c_ulong),
        ("ReasonStringCount", ctypes.c_ulong),
        ("ReasonStrings", ctypes.c_void_p),
    ]


class _Reason(ctypes.Union):
    _fields_ = [("Detailed", _Detailed), ("SimpleReasonString", ctypes.c_wchar_p)]


class REASON_CONTEXT(ctypes.Structure):  # noqa: N801 - Windows name
    _fields_ = [("Version", ctypes.c_ulong), ("Flags", ctypes.c_ulong), ("Reason", _Reason)]


def _kernel32() -> Any:
    kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)  # type: ignore[attr-defined]
    kernel32.PowerCreateRequest.restype = ctypes.c_void_p
    kernel32.PowerCreateRequest.argtypes = [ctypes.POINTER(REASON_CONTEXT)]
    kernel32.PowerSetRequest.restype = ctypes.c_int
    kernel32.PowerSetRequest.argtypes = [ctypes.c_void_p, ctypes.c_int]
    kernel32.PowerClearRequest.restype = ctypes.c_int
    kernel32.PowerClearRequest.argtypes = [ctypes.c_void_p, ctypes.c_int]
    kernel32.CloseHandle.argtypes = [ctypes.c_void_p]
    kernel32.SetThreadExecutionState.restype = ctypes.c_uint
    kernel32.SetThreadExecutionState.argtypes = [ctypes.c_uint]
    return kernel32


class KeepAwake:
    """Holds the power request between ``start()`` and ``stop()``."""

    def __init__(self, *, enabled: bool = True, platform: str = sys.platform, kernel32: Any = None) -> None:
        self._enabled = enabled
        self._platform = platform
        self._kernel32 = kernel32
        self._handle: Any = None
        self._thread_state = False

    @property
    def active(self) -> bool:
        return self._handle is not None or self._thread_state

    def start(self) -> bool:
        """Ask Windows not to sleep. Returns True when it accepted."""

        if self.active or not self._enabled or self._platform != "win32":
            return self.active
        try:
            kernel32 = self._kernel32 if self._kernel32 is not None else _kernel32()
            self._kernel32 = kernel32
            context = REASON_CONTEXT(POWER_REQUEST_CONTEXT_VERSION, POWER_REQUEST_CONTEXT_SIMPLE_STRING)
            # Windows copies the reason when it creates the request.
            context.Reason.SimpleReasonString = REASON
            handle = kernel32.PowerCreateRequest(ctypes.byref(context))
            if handle and handle != INVALID_HANDLE_VALUE:
                if kernel32.PowerSetRequest(handle, POWER_REQUEST_SYSTEM_REQUIRED):
                    self._handle = handle
                    _LOG.info("keeping this computer awake while the site box runs")
                    return True
                kernel32.CloseHandle(handle)
            # Older or restricted systems: the per-thread setting does the same.
            if kernel32.SetThreadExecutionState(ES_CONTINUOUS | ES_SYSTEM_REQUIRED):
                self._thread_state = True
                _LOG.info("keeping this computer awake while the site box runs")
                return True
        except (AttributeError, OSError) as error:
            _LOG.warning("could not keep this computer awake: %s", error)
            return False
        _LOG.warning("Windows refused to keep this computer awake; set it to never sleep in Power settings")
        return False

    def stop(self) -> None:
        """Let the computer sleep normally again."""

        kernel32 = self._kernel32
        if kernel32 is None:
            return
        try:
            if self._handle is not None:
                kernel32.PowerClearRequest(self._handle, POWER_REQUEST_SYSTEM_REQUIRED)
                kernel32.CloseHandle(self._handle)
            if self._thread_state:
                kernel32.SetThreadExecutionState(ES_CONTINUOUS)
        except OSError as error:
            _LOG.debug("releasing the power request failed: %s", error)
        finally:
            self._handle = None
            self._thread_state = False
