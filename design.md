# Requirements 

* Trustless 
* Handles computers going offline and online for extended periods of time 
* Cost effective 

# Design Overview

* Client keeps local Set<HistoryEntry> in sqlite. HistoryEntry has usual metadata + an ID that is incrementing per machine 
* Client has USER_SECRET 
* Client gets installed with USER_SECRET
* Client asks backend for N, number of registered computers for HMAC(USER_SECRET, "comp_count"). It tells the backend to increment this. 
    * Periodically it checks the backend for a new N 
* Sends message to the queue named HMAC(USER_SECRET,i) for 0 --> N asking for history dump 
* Every time `hishtory query` is run, it checks HMAC(USER_SECRET,i) for 0 --> N for new history entries 
* Every time it records a hishtory entry (or batched shortly afterwards) it sends that entry to all queues 
* With some frequency (once a day?) it reads all queues for new history entries

# Offline Recovery Flow 

* Once-per-day, it checks if it doesn't have any history entries from other computers within the past EXPIRY days. If it doesn't, it sends a dump request to all queues, then receives their responses 



## SQS/PubSub/Redis/Postgres Queue