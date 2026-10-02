While keeping this repo very usable in trade_orchestrator finds ways to improve it.

Find stale code, and remove it. 

Suggest ways to improve the pipeline:
Study hopefully yields a pattern to turn into a Strategy. 
Backtesting a strategy yields confirmation that a strategy is worht improving. 
Gridsearch finds optimal parameters for a given strategy.

I've been doing all this quite manually.  What is needed to automate pattern finding and creating a strategy with the pattern.


We have 

Todo: 
Search is there partly:
"The best stack so far hits 79.9% CAGR in-sample but with 19.7% drawdown (Calmar 4.05), well above the under-6% target. I'll sweep primary and position size settings to map the CAGR/drawdown"

Make it easy run several sibling strategies without a designated primary. 
Can the park asset logic be a strategy that gets stacked like other strategies?
If the drawdown is because 2022 great?

I hate that there are multiple report folders, and multiple data folders. 
Probably an execution environment setting issue.


Can I improve any strategies by avoiding earnings reports? How often do earnings reports come out the on they are scheduled? 
Who has the report calendar as a data feed? 


How do I make $20k this month with a $109k base? 


We now have 30,000+ strategies so creating a file per strategy per backtest run is annoying.  What you do recommend?
Going to use WAL and concurrency to make big db.
