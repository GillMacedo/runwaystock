# 0001: Use the standard reorder point formula

**Decision:** demand during lead time plus safety stock from the standard deviation of daily sales, with the z value from a service level.

**Why:** people can check it by hand, and it's the textbook method, so results are explainable.

**Trade-off:** it ignores seasonality and assumes fixed lead times, which the README states.

**Alternatives:** machine learning forecasts, which need much more data and can't be explained or tested as simply.
