Using the configuration scripts (Ubuntu/Debian-based systems)
In /etc/network/if-up.d/ you can place scripts that will be executed each time a new interface comes up. We created two scripts:

  * mptcp_up - Place it inside /etc/network/if-up.d/ and make it executable.
  * mptcp_down - Place it inside /etc/network/if-post-down.d/ and make it executable.